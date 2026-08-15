package download

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// RemoteFile describes a single file to download from HuggingFace.
type RemoteFile struct {
	RepoID   string // e.g. "ggml-org/gemma-4-26B-A4B-it-GGUF"
	Filename string // e.g. "gemma-4-26B-A4B-it-Q8_0.gguf"
	Size     int64  // bytes, from HF API
	IsMmproj bool   // true for mmproj-* files (multimodal projector)
}

// hfTreeEntry is one entry from the HF /tree/main endpoint.
type hfTreeEntry struct {
	Type string `json:"type"` // "file" or "directory"
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// ResolveFiles hits the HuggingFace API to find .gguf files matching the
// requested quantization, plus any mmproj file for multimodal models.
//
// It returns the main GGUF and (if present) the mmproj companion.
// Files prefixed with "mtp-" or "dflash-" are skipped — those are
// alternative formats not needed by llama.cpp.
func ResolveFiles(repo, quant string) ([]RemoteFile, error) {
	url := fmt.Sprintf("https://huggingface.co/api/models/%s/tree/main", repo)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if tok := hfToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching HF tree: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HF API returned %d: %s", resp.StatusCode, string(body))
	}

	var entries []hfTreeEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("parsing HF tree JSON: %w", err)
	}

	var mainFile *RemoteFile
	var mmprojFile *RemoteFile

	for _, e := range entries {
		if e.Type != "file" || !strings.HasSuffix(e.Path, ".gguf") {
			continue
		}
		if !strings.Contains(e.Path, quant) {
			continue
		}

		lower := strings.ToLower(e.Path)

		// Skip non-standard prefixes.
		if strings.HasPrefix(lower, "mtp-") || strings.HasPrefix(lower, "dflash-") {
			continue
		}

		if strings.HasPrefix(lower, "mmproj-") {
			mmprojFile = &RemoteFile{
				RepoID:   repo,
				Filename: e.Path,
				Size:     e.Size,
				IsMmproj: true,
			}
		} else {
			mainFile = &RemoteFile{
				RepoID:   repo,
				Filename: e.Path,
				Size:     e.Size,
				IsMmproj: false,
			}
		}
	}

	if mainFile == nil {
		return nil, fmt.Errorf("no .gguf file matching quant %q found in %s", quant, repo)
	}

	var files []RemoteFile
	files = append(files, *mainFile)
	if mmprojFile != nil {
		files = append(files, *mmprojFile)
	}
	return files, nil
}

func hfToken() string {
	if t := os.Getenv("HF_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("HUGGINGFACE_HUB_TOKEN")
}

// ListGGUFFiles fetches the file tree for a HuggingFace repo and returns
// all .gguf files with their sizes. Useful for prompting the user to pick
// a specific quantization from a --link URL that points to a repo page.
func ListGGUFFiles(repo string) ([]RemoteFile, error) {
	url := fmt.Sprintf("https://huggingface.co/api/models/%s/tree/main", repo)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if tok := hfToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching HF tree: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HF API returned %d: %s", resp.StatusCode, string(body))
	}

	var entries []hfTreeEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("parsing HF tree JSON: %w", err)
	}

	var files []RemoteFile
	for _, e := range entries {
		if e.Type != "file" || !strings.HasSuffix(e.Path, ".gguf") {
			continue
		}
		// Skip non-standard prefixes.
		lower := strings.ToLower(e.Path)
		if strings.HasPrefix(lower, "mtp-") || strings.HasPrefix(lower, "dflash-") {
			continue
		}
		files = append(files, RemoteFile{
			RepoID:   repo,
			Filename: e.Path,
			Size:     e.Size,
			IsMmproj: strings.HasPrefix(lower, "mmproj-"),
		})
	}

	return files, nil
}
