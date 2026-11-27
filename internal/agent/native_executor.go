package agent

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/orchestrator"
)

type NativeExecutor struct {
	workDir       string
	runnerVersion string
	cacheEnabled  bool
	httpClient    *http.Client
	cache         *orchestrator.CacheManager

	mu            sync.Mutex
	cachedVersion string
	cachedAt      time.Time
}

func NewNativeExecutor(workDir, runnerVersion string, cacheEnabled bool) *NativeExecutor {
	return &NativeExecutor{
		workDir:       workDir,
		runnerVersion: strings.TrimPrefix(strings.TrimSpace(runnerVersion), "v"),
		cacheEnabled:  cacheEnabled,
		httpClient:    &http.Client{Timeout: 5 * time.Minute},
		cache:         orchestrator.NewCacheManager(),
	}
}

func (e *NativeExecutor) CacheStats() (hits, misses int64) {
	if e.cache == nil {
		return 0, 0
	}
	return e.cache.Stats()
}

func (e *NativeExecutor) Execute(ctx context.Context, task *TaskPayload) error {
	return e.ExecuteWithLogs(ctx, task, nil)
}

func (e *NativeExecutor) ExecuteWithLogs(ctx context.Context, task *TaskPayload, logSink func([]domain.RunnerLogEntry)) error {
	if strings.TrimSpace(task.EncodedJITConfig) == "" {
		return fmt.Errorf("task missing encoded_jit_config")
	}

	templateDir, err := e.ensureUnpackedTemplate(ctx)
	if err != nil {
		return err
	}

	jobDir := filepath.Join(e.workDir, "jobs", sanitizePathSegment(task.RunnerName))
	_ = os.RemoveAll(jobDir)
	if err := cloneTemplateDir(templateDir, jobDir); err != nil {
		return fmt.Errorf("prepare job directory from template: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(jobDir)
	}()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/C", "run.cmd", "--jitconfig", task.EncodedJITConfig)
	} else {
		runScript := filepath.Join(jobDir, "run.sh")
		_ = os.Chmod(runScript, 0o755)
		cmd = exec.CommandContext(ctx, "/bin/sh", runScript, "--jitconfig", task.EncodedJITConfig)
	}
	cmd.Dir = jobDir
	env := append(os.Environ(), "CI=true")
	if e.cacheEnabled && task.CacheEnabled && !task.IsForkPR && strings.ToLower(strings.TrimSpace(task.CacheScope)) != "none" && e.cache != nil {
		scopeKey := e.cache.ScopeKey(task.PoolSlug, task.RepoFullName, task.CacheScope)
		if scopeKey != "" {
			cacheRoot := filepath.Join(e.workDir, "build-cache", scopeKey)
			_, statErr := os.Stat(cacheRoot)
			existed := statErr == nil
			if mkErr := os.MkdirAll(cacheRoot, 0o755); mkErr == nil {
				e.cache.TrackVolumeHitMiss(cacheRoot, existed)
				for _, sub := range []string{"gomod", "gocache", "npm", "bun", "pip", "cargo", "ccache", "sccache", "toolcache"} {
					_ = os.MkdirAll(filepath.Join(cacheRoot, sub), 0o755)
				}
				vcpu := task.VCPU
				if vcpu <= 0 {
					vcpu = runtime.NumCPU()
				}
				env = append(env, orchestrator.CacheEnvVars(cacheRoot, task.BuildProfile, vcpu)...)
			}
		}
	}
	cmd.Env = env

	var outBuf cappedTailBuffer
	outBuf.maxBytes = 8192

	if logSink == nil {
		cmd.Stdout = &outBuf
		cmd.Stderr = &outBuf
		if err := cmd.Run(); err != nil {
			return formatRunnerExitError(err, outBuf.String(), task.EncodedJITConfig)
		}
		return nil
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	var logWG sync.WaitGroup
	logCh := make(chan domain.RunnerLogEntry, 256)
	readStream := func(r io.Reader, streamName, lvl string) {
		defer logWG.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 512*1024)
		for sc.Scan() {
			line := sc.Text()
			if task.EncodedJITConfig != "" {
				line = strings.ReplaceAll(line, task.EncodedJITConfig, "[REDACTED]")
			}
			_, _ = outBuf.Write([]byte(line + "\n"))
			select {
			case logCh <- domain.RunnerLogEntry{
				Timestamp: time.Now().UTC(),
				Stream:    streamName,
				Level:     lvl,
				Line:      line,
			}:
			default:
			}
		}
	}

	shipDone := make(chan struct{})
	go func() {
		defer close(shipDone)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		batch := make([]domain.RunnerLogEntry, 0, 50)
		flush := func() {
			if len(batch) == 0 {
				return
			}
			logSink(append([]domain.RunnerLogEntry(nil), batch...))
			batch = batch[:0]
		}
		for {
			select {
			case entry, ok := <-logCh:
				if !ok {
					flush()
					return
				}
				batch = append(batch, entry)
				if len(batch) >= 50 {
					flush()
				}
			case <-ticker.C:
				flush()
			}
		}
	}()

	logWG.Add(2)
	go readStream(stdoutPipe, "stdout", "info")
	go readStream(stderrPipe, "stderr", "error")
	logWG.Wait()
	close(logCh)
	<-shipDone

	if err := cmd.Wait(); err != nil {
		return formatRunnerExitError(err, outBuf.String(), task.EncodedJITConfig)
	}
	return nil
}

func formatRunnerExitError(err error, tailRaw, secret string) error {
	tail := strings.TrimSpace(tailRaw)
	if tail != "" {
		if secret != "" {
			tail = strings.ReplaceAll(tail, secret, "[REDACTED]")
		}
		return fmt.Errorf("runner exited with %v: %s", err, tail)
	}
	return fmt.Errorf("runner exited: %w", err)
}

// ensureUnpackedTemplate extracts the actions/runner archive once per version so every job clones in milliseconds.
func (e *NativeExecutor) ensureUnpackedTemplate(ctx context.Context) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	ver, err := e.resolveVersionLocked(ctx)
	if err != nil {
		return "", err
	}
	unpackedDir := filepath.Join(e.workDir, "cache", "unpacked", ver+"-"+runtime.GOOS+"-"+runtime.GOARCH)
	sentinel := filepath.Join(unpackedDir, ".runway-ready")
	if _, statErr := os.Stat(sentinel); statErr == nil {
		return unpackedDir, nil
	}

	archivePath, isZip, err := e.ensureRunnerArchiveLocked(ctx, ver)
	if err != nil {
		return "", err
	}
	tmpDir := unpackedDir + ".tmp"
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", fmt.Errorf("create unpacked template dir: %w", err)
	}
	if isZip {
		if err := extractZip(archivePath, tmpDir); err != nil {
			_ = os.RemoveAll(tmpDir)
			return "", fmt.Errorf("extract windows actions-runner: %w", err)
		}
	} else {
		if err := extractTarGz(archivePath, tmpDir); err != nil {
			_ = os.RemoveAll(tmpDir)
			return "", fmt.Errorf("extract actions-runner: %w", err)
		}
	}
	_ = os.WriteFile(filepath.Join(tmpDir, ".runway-ready"), []byte(ver), 0o644)
	_ = os.RemoveAll(unpackedDir)
	if err := os.Rename(tmpDir, unpackedDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("finalize unpacked template dir: %w", err)
	}
	return unpackedDir, nil
}

func cloneTemplateDir(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		if err := exec.Command("cp", "-cR", src, dst).Run(); err == nil {
			return nil
		}
	case "linux":
		if err := exec.Command("cp", "-a", "--reflink=auto", src, dst).Run(); err == nil {
			return nil
		}
	}
	return copyDirRecursive(src, dst)
}

func copyDirRecursive(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode()&0o777)
		}
		if d.Type()&os.ModeSymlink != 0 {
			linkDest, linkErr := os.Readlink(path)
			if linkErr != nil {
				return linkErr
			}
			return os.Symlink(linkDest, target)
		}
		in, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		defer in.Close()
		out, createErr := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode()&0o777)
		if createErr != nil {
			return createErr
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func (e *NativeExecutor) ensureRunnerArchiveLocked(ctx context.Context, ver string) (string, bool, error) {
	filename, isZip, err := actionsRunnerFilename(ver, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", false, err
	}

	cacheDir := filepath.Join(e.workDir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", false, fmt.Errorf("create runner cache dir: %w", err)
	}
	archivePath := filepath.Join(cacheDir, filename)
	if info, statErr := os.Stat(archivePath); statErr == nil && info.Size() > 1<<20 {
		return archivePath, isZip, nil
	}

	downloadURL := fmt.Sprintf("https://github.com/actions/runner/releases/download/v%s/%s", ver, filename)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", "runway-agent")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("download actions/runner %s: %w", ver, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("download actions/runner %s failed (%d)", ver, resp.StatusCode)
	}

	tmpPath := archivePath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", false, err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		return "", false, fmt.Errorf("write actions/runner archive: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", false, err
	}
	if err := os.Rename(tmpPath, archivePath); err != nil {
		_ = os.Remove(tmpPath)
		return "", false, err
	}
	return archivePath, isZip, nil
}

func (e *NativeExecutor) resolveVersionLocked(ctx context.Context) (string, error) {
	if e.runnerVersion != "" {
		return e.runnerVersion, nil
	}
	if e.cachedVersion != "" && time.Since(e.cachedAt) < 6*time.Hour {
		return e.cachedVersion, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/actions/runner/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "runway-agent")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("query latest actions/runner release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("query latest actions/runner release failed (%d)", resp.StatusCode)
	}

	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return "", fmt.Errorf("decode actions/runner release: %w", err)
	}
	ver := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	if ver == "" {
		return "", fmt.Errorf("empty tag_name returned for actions/runner")
	}
	e.cachedVersion = ver
	e.cachedAt = time.Now()
	return ver, nil
}

func actionsRunnerFilename(version, goos, goarch string) (filename string, isZip bool, err error) {
	var osPart string
	switch goos {
	case "linux":
		osPart = "linux"
	case "darwin":
		osPart = "osx"
	case "windows":
		osPart = "win"
		isZip = true
	default:
		return "", false, fmt.Errorf("unsupported host OS %q for actions/runner", goos)
	}

	var archPart string
	switch goarch {
	case "amd64":
		archPart = "x64"
	case "arm64":
		archPart = "arm64"
	default:
		return "", false, fmt.Errorf("unsupported host architecture %q for actions/runner", goarch)
	}

	ext := ".tar.gz"
	if isZip {
		ext = ".zip"
	}
	return fmt.Sprintf("actions-runner-%s-%s-%s%s", osPart, archPart, version, ext), isZip, nil
}

func sanitizePathSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "job"
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func safeJoin(baseDir, entryName string) (string, error) {
	cleanBase := filepath.Clean(baseDir)
	cleanEntry := filepath.Clean(filepath.FromSlash(entryName))
	if cleanEntry == "." || cleanEntry == "" {
		return cleanBase, nil
	}
	target := filepath.Join(cleanBase, cleanEntry)
	rel, err := filepath.Rel(cleanBase, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(cleanEntry) {
		return "", fmt.Errorf("archive entry %q escapes destination", entryName)
	}
	return target, nil
}

func extractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(destDir, hdr.Name)
		if err != nil {
			return err
		}
		if target == filepath.Clean(destDir) {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(hdr.Linkname) {
				continue
			}
			linkDest := filepath.Join(filepath.Dir(target), hdr.Linkname)
			rel, relErr := filepath.Rel(filepath.Clean(destDir), filepath.Clean(linkDest))
			if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
}

func extractZip(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, zf := range r.File {
		target, err := safeJoin(destDir, zf.Name)
		if err != nil {
			return err
		}
		if target == filepath.Clean(destDir) {
			continue
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := zf.Mode() & 0o777
		if mode == 0 {
			mode = 0o644
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		closeErr := out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

type cappedTailBuffer struct {
	maxBytes int
	data     []byte
}

func (b *cappedTailBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	if b.maxBytes > 0 && len(b.data) > b.maxBytes {
		b.data = append([]byte(nil), b.data[len(b.data)-b.maxBytes:]...)
	}
	return len(p), nil
}

func (b *cappedTailBuffer) String() string {
	return string(b.data)
}
