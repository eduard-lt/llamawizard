package download

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Progress is sent on the progress channel during a download.
type Progress struct {
	Downloaded  int64 // bytes downloaded so far
	Total       int64 // total bytes expected (from HF API)
	Filename    string
	BytesPerSec int64 // average download speed since start
}

// hfResolveURL returns the direct download URL for a file on HuggingFace.
var hfResolveURL = func(repoID, filename string) string {
	return fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", repoID, filename)
}

// Download fetches a RemoteFile into destDir, writing to a .partial file
// first and renaming atomically on success. Supports range-resume: if a
// .partial file already exists it sends Range: bytes=<offset>- and appends.
func Download(f RemoteFile, destDir string, progress chan<- Progress) error {
	return downloadWithClient(http.DefaultClient, f, destDir, progress)
}

var progressInterval = 100 * time.Millisecond

type progressReader struct {
	r        io.Reader
	progress chan<- Progress
	filename string
	total    int64
	offset   int64
	lastSent time.Time
	started  time.Time
}

func (pr *progressReader) Read(p []byte) (int, error) {
	if pr.started.IsZero() {
		pr.started = time.Now()
	}
	n, err := pr.r.Read(p)
	pr.offset += int64(n)
	if n > 0 && pr.progress != nil && time.Since(pr.lastSent) > progressInterval {
		elapsed := time.Since(pr.started).Seconds()
		var speed int64
		if elapsed > 0.1 {
			speed = int64(float64(pr.offset) / elapsed)
		}
		pr.progress <- Progress{
			Downloaded:  pr.offset,
			Total:       pr.total,
			Filename:    pr.filename,
			BytesPerSec: speed,
		}
		pr.lastSent = time.Now()
	}
	return n, err
}

func downloadWithClient(client *http.Client, f RemoteFile, destDir string, progress chan<- Progress) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("creating dest dir: %w", err)
	}

	partialPath := filepath.Join(destDir, f.Filename+".partial")
	finalPath := filepath.Join(destDir, f.Filename)

	if stat, err := os.Stat(finalPath); err == nil {
		if f.Size > 0 && stat.Size() == f.Size {
			if progress != nil {
				progress <- Progress{
					Filename:    f.Filename,
					Downloaded:  f.Size,
					Total:       f.Size,
					BytesPerSec: 0,
				}
			}
			return nil
		}
	}

	var offset int64
	if stat, err := os.Stat(partialPath); err == nil {
		offset = stat.Size()
	}

	url := hfResolveURL(f.RepoID, f.Filename)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	if tok := hfToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download returned %d: %s", resp.StatusCode, string(body))
	}

	// Open .partial for writing (create or append).
	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	fw, err := os.OpenFile(partialPath, flags, 0o644)
	if err != nil {
		return fmt.Errorf("opening partial file: %w", err)
	}
	defer func() { _ = fw.Close() }()

	pr := &progressReader{
		r:        resp.Body,
		progress: progress,
		total:    f.Size,
		filename: f.Filename,
		offset:   offset,
	}
	written, err := io.Copy(fw, pr)
	if err != nil {
		// Leave .partial in place so the next call can resume.
		return fmt.Errorf("writing file: %w", err)
	}

	totalDownloaded := offset + written

	// Send final progress.
	if progress != nil {
		elapsed := time.Since(pr.started).Seconds()
		var speed int64
		if elapsed > 0.1 {
			speed = int64(float64(totalDownloaded) / elapsed)
		}
		progress <- Progress{
			Downloaded:  totalDownloaded,
			Total:       f.Size,
			Filename:    f.Filename,
			BytesPerSec: speed,
		}
	}

	// Verify size matches what the HF API reported.
	if f.Size > 0 && totalDownloaded != f.Size {
		return fmt.Errorf("size mismatch: expected %d bytes, got %d (file may be truncated)", f.Size, totalDownloaded)
	}

	// Atomic rename from .partial to final filename.
	if err := os.Rename(partialPath, finalPath); err != nil {
		return fmt.Errorf("renaming partial to final: %w", err)
	}

	_ = finalPath // referenced above
	return nil
}
