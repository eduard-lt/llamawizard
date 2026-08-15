package download

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testPayload = []byte("hello world, this is a test payload for llamawizard download logic\n")

// mockHFServer creates an HTTP server that mimics HF's /resolve/main/ endpoint.
// It supports Range requests for resume testing.
func mockHFServer(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}

		if !strings.HasPrefix(rangeHeader, "bytes=") {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		offsetStr := strings.TrimPrefix(rangeHeader, "bytes=")
		idx := strings.Index(offsetStr, "-")
		if idx >= 0 {
			offsetStr = offsetStr[:idx]
		}
		var offset int64
		_, _ = fmt.Sscanf(offsetStr, "%d", &offset)

		if offset >= int64(len(data)) || offset < 0 {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}

		remaining := data[offset:]
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, int64(len(data))-1, len(data)))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(remaining)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(remaining)
	}))
}

// TestDownloadComplete downloads a file from a mock server and verifies content.
func TestDownloadComplete(t *testing.T) {
	ts := mockHFServer(t, testPayload)
	defer ts.Close()

	destDir := t.TempDir()
	progressCh := make(chan Progress, 10)

	origURL := hfResolveURL
	hfResolveURL = func(_, _ string) string { return ts.URL }
	defer func() { hfResolveURL = origURL }()

	f := RemoteFile{
		Filename: "model.gguf",
		Size:     int64(len(testPayload)),
	}

	err := Download(f, destDir, progressCh)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	finalPath := filepath.Join(destDir, f.Filename)
	data, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("reading final file: %v", err)
	}
	if !bytes.Equal(data, testPayload) {
		t.Errorf("content mismatch: got %d bytes", len(data))
	}

	// .partial should be gone.
	partialPath := filepath.Join(destDir, f.Filename+".partial")
	if _, err := os.Stat(partialPath); !os.IsNotExist(err) {
		t.Error(".partial still exists after successful download")
	}

	// Check progress was sent.
	select {
	case p := <-progressCh:
		if p.Downloaded != int64(len(testPayload)) {
			t.Errorf("progress downloaded: want %d, got %d", len(testPayload), p.Downloaded)
		}
		if p.Total != int64(len(testPayload)) {
			t.Errorf("progress total: want %d, got %d", len(testPayload), p.Total)
		}
	default:
		t.Error("no progress sent")
	}
}

// TestDownloadResume simulates a killed download and verifies resume works.
func TestDownloadResume(t *testing.T) {
	ts := mockHFServer(t, testPayload)
	defer ts.Close()

	destDir := t.TempDir()
	progressCh := make(chan Progress, 10)

	origURL := hfResolveURL
	hfResolveURL = func(_, _ string) string { return ts.URL }
	defer func() { hfResolveURL = origURL }()

	f := RemoteFile{
		Filename: "model.gguf",
		Size:     int64(len(testPayload)),
	}

	// Step 1: Write a .partial file (simulating an interrupted download).
	half := len(testPayload) / 2
	partialPath := filepath.Join(destDir, f.Filename+".partial")
	if err := os.WriteFile(partialPath, testPayload[:half], 0o644); err != nil {
		t.Fatalf("writing partial: %v", err)
	}

	// Step 2: Call Download — it should detect .partial and resume via Range.
	err := Download(f, destDir, progressCh)
	if err != nil {
		t.Fatalf("Download (resume) failed: %v", err)
	}

	// Step 3: Verify final file is complete and correct.
	finalPath := filepath.Join(destDir, f.Filename)
	data, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("reading final file: %v", err)
	}
	if !bytes.Equal(data, testPayload) {
		t.Errorf("content mismatch after resume: got %d bytes, want %d", len(data), len(testPayload))
	}

	// .partial should be gone.
	if _, err := os.Stat(partialPath); !os.IsNotExist(err) {
		t.Error(".partial still exists after resumed download")
	}
}

// TestDownloadSizeMismatch verifies that size verification catches truncated files.
func TestDownloadSizeMismatch(t *testing.T) {
	// Mock server returns only half the data but the RemoteFile claims full size.
	ts := mockHFServer(t, testPayload[:len(testPayload)/2])
	defer ts.Close()

	destDir := t.TempDir()
	progressCh := make(chan Progress, 10)

	origURL := hfResolveURL
	hfResolveURL = func(_, _ string) string { return ts.URL }
	defer func() { hfResolveURL = origURL }()

	f := RemoteFile{
		Filename: "model.gguf",
		Size:     int64(len(testPayload)), // claims full size, server sends half
	}

	err := Download(f, destDir, progressCh)
	if err == nil {
		t.Fatal("expected error for size mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "size mismatch") {
		t.Errorf("expected size mismatch error, got: %v", err)
	}
}

// TestDownloadPartialCleanup verifies .partial is removed after successful rename.
func TestDownloadPartialCleanup(t *testing.T) {
	ts := mockHFServer(t, testPayload)
	defer ts.Close()

	destDir := t.TempDir()
	progressCh := make(chan Progress, 10)

	origURL := hfResolveURL
	hfResolveURL = func(_, _ string) string { return ts.URL }
	defer func() { hfResolveURL = origURL }()

	f := RemoteFile{
		Filename: "model.gguf",
		Size:     int64(len(testPayload)),
	}

	if err := Download(f, destDir, progressCh); err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	partialPath := filepath.Join(destDir, f.Filename+".partial")
	if _, err := os.Stat(partialPath); !os.IsNotExist(err) {
		t.Error(".partial still exists after rename")
	}
}

// TestDownloadCreatesDestDir verifies dest directory is created if missing.
func TestDownloadCreatesDestDir(t *testing.T) {
	ts := mockHFServer(t, testPayload)
	defer ts.Close()

	tmpDir := t.TempDir()
	destDir := filepath.Join(tmpDir, "nested", "deep", "models")
	progressCh := make(chan Progress, 10)

	origURL := hfResolveURL
	hfResolveURL = func(_, _ string) string { return ts.URL }
	defer func() { hfResolveURL = origURL }()

	f := RemoteFile{
		Filename: "model.gguf",
		Size:     int64(len(testPayload)),
	}

	if err := Download(f, destDir, progressCh); err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	finalPath := filepath.Join(destDir, f.Filename)
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("final file not found: %v", err)
	}
}

// TestDownloadNoProgressChannel verifies Download works with nil progress channel.
func TestDownloadNoProgressChannel(t *testing.T) {
	ts := mockHFServer(t, testPayload)
	defer ts.Close()

	destDir := t.TempDir()

	origURL := hfResolveURL
	hfResolveURL = func(_, _ string) string { return ts.URL }
	defer func() { hfResolveURL = origURL }()

	f := RemoteFile{
		Filename: "model.gguf",
		Size:     int64(len(testPayload)),
	}

	if err := Download(f, destDir, nil); err != nil {
		t.Fatalf("Download with nil progress failed: %v", err)
	}
}

// TestMockServerRangeRequests verifies the mock server correctly handles Range.
func TestMockServerRangeRequests(t *testing.T) {
	ts := mockHFServer(t, testPayload)
	defer ts.Close()

	// Full request.
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("full request: %v", err)
	}
	fullBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if !bytes.Equal(fullBody, testPayload) {
		t.Errorf("full download mismatch")
	}

	// Range request for bytes 5-.
	req, _ := http.NewRequest("GET", ts.URL, nil)
	req.Header.Set("Range", "bytes=5-")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("range request: %v", err)
	}
	rangeBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Errorf("expected 206 Partial Content, got %d", resp.StatusCode)
	}
	if !bytes.Equal(rangeBody, testPayload[5:]) {
		t.Errorf("range download mismatch: got %d bytes, want %d", len(rangeBody), len(testPayload)-5)
	}
}

func TestDownload_SkipsWhenFileAlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	destDir := filepath.Join(tmpDir, "model")

	data := []byte("original file content that should not be overwritten")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	finalPath := filepath.Join(destDir, "model.gguf")
	if err := os.WriteFile(finalPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	f := RemoteFile{
		RepoID:   "user/model",
		Filename: "model.gguf",
		Size:     int64(len(data)),
	}
	err := Download(f, destDir, nil)

	if err != nil {
		t.Fatalf("Download returned error: %v", err)
	}
	readBack, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readBack, data) {
		t.Errorf("file was overwritten: got %q, want %q", readBack, data)
	}
}

func TestDownload_SkipsWhenFileSizeMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	destDir := filepath.Join(tmpDir, "model")

	data := []byte("partial content")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	finalPath := filepath.Join(destDir, "model.gguf")
	if err := os.WriteFile(finalPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	f := RemoteFile{
		RepoID:   "user/model",
		Filename: "model.gguf",
		Size:     int64(len(data) + 100), // claim file should be bigger
	}
	err := Download(f, destDir, nil)

	// Should attempt to download because sizes don't match.
	// Since no mock server is set, it'll fail with a connection error,
	// which confirms the skip was NOT triggered.
	if err == nil {
		t.Error("expected error when file size mismatches, got nil")
	}
}

func TestDownloadURL_ResumesFromPartial(t *testing.T) {
	ts := mockHFServer(t, testPayload)
	defer ts.Close()

	destDir := t.TempDir()

	half := len(testPayload) / 2
	partialPath := filepath.Join(destDir, "model.gguf.partial")
	if err := os.WriteFile(partialPath, testPayload[:half], 0o644); err != nil {
		t.Fatal(err)
	}

	if err := DownloadURL(ts.URL, "model.gguf", int64(len(testPayload)), destDir, nil); err != nil {
		t.Fatalf("DownloadURL failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(destDir, "model.gguf"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, testPayload) {
		t.Errorf("content mismatch after resume: got %q", data)
	}
	if _, err := os.Stat(partialPath); !os.IsNotExist(err) {
		t.Error(".partial still exists after successful resume")
	}
}

func TestDownloadURL_RestartsWhenRangeIgnored(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testPayload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(testPayload)
	}))
	defer ts.Close()

	destDir := t.TempDir()
	partialPath := filepath.Join(destDir, "model.gguf.partial")
	if err := os.WriteFile(partialPath, []byte("garbage partial data"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := DownloadURL(ts.URL, "model.gguf", int64(len(testPayload)), destDir, nil); err != nil {
		t.Fatalf("DownloadURL failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(destDir, "model.gguf"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, testPayload) {
		t.Errorf("content mismatch after range-ignored restart: got %q", data)
	}
}
