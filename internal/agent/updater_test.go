package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSemverComparison(t *testing.T) {
	if !isNewerVersion("v0.1.0", "v0.1.1") {
		t.Fatal("expected v0.1.1 > v0.1.0")
	}
	if !isNewerVersion("0.1.0", "v1.0.0") {
		t.Fatal("expected v1.0.0 > 0.1.0")
	}
	if isNewerVersion("v1.2.0", "v1.2.0") {
		t.Fatal("expected v1.2.0 == v1.2.0")
	}
	if isNewerVersion("v1.3.0", "v1.2.9") {
		t.Fatal("expected v1.2.9 < v1.3.0")
	}
	if isNewerVersion("dev", "v9.9.9") {
		t.Fatal("dev builds should not auto-update unless forced")
	}
}

func TestSafeJoinPreventsTraversal(t *testing.T) {
	dest := t.TempDir()
	if _, err := safeJoin(dest, "../../etc/passwd"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
	got, err := safeJoin(dest, "bin/run.sh")
	if err != nil || got != filepath.Join(dest, "bin", "run.sh") {
		t.Fatalf("expected valid path under dest, got %q err=%v", got, err)
	}
}

func TestUpdaterCheckAndApply(t *testing.T) {
	newPayload := []byte("#!/bin/sh\necho updated-agent\n")
	var tarBuf bytes.Buffer
	gzw := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gzw)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "runway-agent",
		Mode:     0o755,
		Size:     int64(len(newPayload)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(newPayload); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gzw.Close()

	archiveBytes := tarBuf.Bytes()
	sum := sha256.Sum256(archiveBytes)
	hexSum := hex.EncodeToString(sum[:])

	assetName, _, err := agentAssetFilename("1.2.0", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}

	tamper := false
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/cubetiqlabs/runway/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v1.2.0",
				"assets": []map[string]string{
					{"name": assetName, "browser_download_url": ts.URL + "/dl/" + assetName},
					{"name": "checksums.txt", "browser_download_url": ts.URL + "/dl/checksums.txt"},
				},
			})
		case "/dl/" + assetName:
			if tamper {
				_, _ = w.Write([]byte("corrupted"))
				return
			}
			_, _ = w.Write(archiveBytes)
		case "/dl/checksums.txt":
			_, _ = w.Write([]byte(hexSum + "  " + assetName + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	targetBin := filepath.Join(t.TempDir(), "runway-agent")
	if err := os.WriteFile(targetBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	u := &Updater{
		repo:       "cubetiqlabs/runway",
		apiBaseURL: ts.URL,
		goos:       "linux",
		goarch:     "amd64",
		httpClient: ts.Client(),
	}

	// Tampered download must fail checksum verification without replacing target.
	tamper = true
	if _, err := u.CheckAndApplyToPath("v1.0.0", false, targetBin); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected sha256 mismatch error, got: %v", err)
	}
	if b, _ := os.ReadFile(targetBin); string(b) != "old-binary" {
		t.Fatalf("expected old-binary to remain intact after failed checksum, got %q", string(b))
	}

	// Valid download must replace target atomically.
	tamper = false
	res, err := u.CheckAndApplyToPath("v1.0.0", false, targetBin)
	if err != nil {
		t.Fatalf("expected update to succeed, got: %v", err)
	}
	if !res.Updated || res.LatestVersion != "v1.2.0" {
		t.Fatalf("unexpected update result: %+v", res)
	}
	if b, _ := os.ReadFile(targetBin); string(b) != string(newPayload) {
		t.Fatalf("expected replaced binary contents, got %q", string(b))
	}
}
