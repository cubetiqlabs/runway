package agent

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Updater struct {
	repo       string
	apiBaseURL string
	goos       string
	goarch     string
	httpClient *http.Client
}

func NewUpdater(repo string) *Updater {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		repo = "cubetiqlabs/runway"
	}
	return &Updater{
		repo:       repo,
		apiBaseURL: "https://api.github.com",
		goos:       runtime.GOOS,
		goarch:     runtime.GOARCH,
		httpClient: &http.Client{Timeout: 2 * time.Minute},
	}
}

type UpdateResult struct {
	Updated       bool
	LatestVersion string
}

// CleanupOldBinary removes stale .old backups left behind after a Windows self-update.
func CleanupOldBinary() {
	if exePath, err := os.Executable(); err == nil {
		_ = os.Remove(exePath + ".old")
	}
}

func (u *Updater) CheckAndApply(currentVersion string, force bool) (UpdateResult, error) {
	exePath, err := os.Executable()
	if err != nil {
		return UpdateResult{}, fmt.Errorf("resolve executable path: %w", err)
	}
	return u.CheckAndApplyToPath(currentVersion, force, exePath)
}

func (u *Updater) CheckAndApplyToPath(currentVersion string, force bool, targetPath string) (UpdateResult, error) {
	rel, err := u.fetchLatestRelease()
	if err != nil {
		return UpdateResult{}, err
	}
	if !force && !isNewerVersion(currentVersion, rel.TagName) {
		return UpdateResult{Updated: false, LatestVersion: rel.TagName}, nil
	}

	verNoV := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	assetName, isZip, err := agentAssetFilename(verNoV, u.goos, u.goarch)
	if err != nil {
		return UpdateResult{}, err
	}

	var assetURL, checksumsURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case assetName:
			assetURL = a.BrowserDownloadURL
		case "checksums.txt":
			checksumsURL = a.BrowserDownloadURL
		}
	}
	if assetURL == "" {
		return UpdateResult{}, fmt.Errorf("release %s does not contain asset %s", rel.TagName, assetName)
	}
	if checksumsURL == "" {
		return UpdateResult{}, fmt.Errorf("release %s does not contain checksums.txt", rel.TagName)
	}

	checksumRaw, err := u.downloadBytes(checksumsURL, 1<<20)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("download checksums.txt: %w", err)
	}
	expectedHash, ok := parseChecksumForFile(string(checksumRaw), assetName)
	if !ok {
		return UpdateResult{}, fmt.Errorf("checksums.txt missing entry for %s", assetName)
	}

	tmpDir, err := os.MkdirTemp("", "runway-agent-update-*")
	if err != nil {
		return UpdateResult{}, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	archivePath := filepath.Join(tmpDir, assetName)
	if err := u.downloadToFile(assetURL, archivePath); err != nil {
		return UpdateResult{}, fmt.Errorf("download %s: %w", assetName, err)
	}
	if err := verifyFileSHA256(archivePath, expectedHash); err != nil {
		return UpdateResult{}, err
	}

	binName := "runway-agent"
	if u.goos == "windows" {
		binName = "runway-agent.exe"
	}
	extractedBin, err := extractBinaryFromArchive(archivePath, tmpDir, binName, isZip)
	if err != nil {
		return UpdateResult{}, err
	}
	if err := replaceBinaryAtomically(extractedBin, targetPath); err != nil {
		return UpdateResult{}, err
	}
	return UpdateResult{Updated: true, LatestVersion: rel.TagName}, nil
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func (u *Updater) fetchLatestRelease() (*ghRelease, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(u.apiBaseURL, "/"), u.repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "runway-agent")

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch latest release failed (%d)", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release metadata: %w", err)
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return nil, fmt.Errorf("empty release tag_name")
	}
	return &rel, nil
}

func (u *Updater) downloadBytes(rawURL string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "runway-agent")
	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBytes))
}

func (u *Updater) downloadToFile(rawURL, destPath string) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "runway-agent")
	resp, err := u.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func agentAssetFilename(version, goos, goarch string) (string, bool, error) {
	switch goos {
	case "linux", "darwin", "windows":
	default:
		return "", false, fmt.Errorf("unsupported OS %q", goos)
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return "", false, fmt.Errorf("unsupported architecture %q", goarch)
	}
	if goos == "windows" {
		return fmt.Sprintf("runway-agent_%s_%s_%s.zip", version, goos, goarch), true, nil
	}
	return fmt.Sprintf("runway-agent_%s_%s_%s.tar.gz", version, goos, goarch), false, nil
}

func isNewerVersion(current, latest string) bool {
	currNorm := strings.TrimSpace(current)
	latNorm := strings.TrimSpace(latest)
	if currNorm == "" || strings.EqualFold(currNorm, "dev") {
		return false
	}
	cParts, cOK := parseSemver(currNorm)
	lParts, lOK := parseSemver(latNorm)
	if !cOK || !lOK {
		return false
	}
	for i := 0; i < 3; i++ {
		if lParts[i] > cParts[i] {
			return true
		}
		if lParts[i] < cParts[i] {
			return false
		}
	}
	return false
}

func parseSemver(raw string) ([3]int, bool) {
	var parts [3]int
	s := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	if dash := strings.IndexAny(s, "-+"); dash >= 0 {
		s = s[:dash]
	}
	segs := strings.Split(s, ".")
	if len(segs) != 3 {
		return parts, false
	}
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(segs[i])
		if err != nil || n < 0 {
			return parts, false
		}
		parts[i] = n
	}
	return parts, true
}

func parseChecksumForFile(content, filename string) (string, bool) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == filename {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}

func verifyFileSHA256(filePath, expectedHex string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, strings.TrimSpace(expectedHex)) {
		return fmt.Errorf("sha256 mismatch for %s: expected %s, got %s", filepath.Base(filePath), expectedHex, actual)
	}
	return nil
}

func extractBinaryFromArchive(archivePath, destDir, binaryName string, isZip bool) (string, error) {
	outPath := filepath.Join(destDir, binaryName)
	if isZip {
		r, err := zip.OpenReader(archivePath)
		if err != nil {
			return "", err
		}
		defer r.Close()
		for _, zf := range r.File {
			if filepath.Base(zf.Name) != binaryName || zf.FileInfo().IsDir() {
				continue
			}
			rc, err := zf.Open()
			if err != nil {
				return "", err
			}
			err = writeExecutableFile(outPath, rc)
			rc.Close()
			if err != nil {
				return "", err
			}
			return outPath, nil
		}
		return "", fmt.Errorf("binary %q not found in zip archive", binaryName)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == binaryName {
			if err := writeExecutableFile(outPath, tr); err != nil {
				return "", err
			}
			return outPath, nil
		}
	}
	return "", fmt.Errorf("binary %q not found in tar.gz archive", binaryName)
}

func writeExecutableFile(dest string, src io.Reader) error {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, src); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func replaceBinaryAtomically(srcBin, targetPath string) error {
	targetDir := filepath.Dir(targetPath)
	stagedPath := filepath.Join(targetDir, fmt.Sprintf(".%s.new.%d", filepath.Base(targetPath), time.Now().UnixNano()))
	src, err := os.Open(srcBin)
	if err != nil {
		return err
	}
	err = writeExecutableFile(stagedPath, src)
	src.Close()
	if err != nil {
		return fmt.Errorf("stage updated binary: %w", err)
	}

	if runtime.GOOS == "windows" {
		oldPath := targetPath + ".old"
		_ = os.Remove(oldPath)
		if _, statErr := os.Stat(targetPath); statErr == nil {
			if err := os.Rename(targetPath, oldPath); err != nil {
				_ = os.Remove(stagedPath)
				return fmt.Errorf("backup running binary: %w", err)
			}
		}
	}
	if err := os.Rename(stagedPath, targetPath); err != nil {
		_ = os.Remove(stagedPath)
		return fmt.Errorf("replace binary: %w", err)
	}
	return nil
}
