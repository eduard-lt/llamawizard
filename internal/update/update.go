package update

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var ErrNoReleases = errors.New("no releases found on GitHub")

const (
	repoOwner = "eduard-lt"
	repoName  = "llamawizard"
)

var apiBaseURL = "https://api.github.com"

type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func IsNewer(current, latest string) bool {
	if current == "dev" {
		return latest != ""
	}

	a := strings.TrimPrefix(current, "v")
	b := strings.TrimPrefix(latest, "v")

	aParts := padParts(strings.Split(a, "."))
	bParts := padParts(strings.Split(b, "."))

	for i := 0; i < 3; i++ {
		an, _ := strconv.Atoi(aParts[i])
		bn, _ := strconv.Atoi(bParts[i])
		if bn > an {
			return true
		}
		if bn < an {
			return false
		}
	}
	return false
}

func padParts(parts []string) []string {
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	return parts
}

func CheckLatest() (*Release, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", apiBaseURL, repoOwner, repoName)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "llamawizard")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNoReleases
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decoding release info: %w", err)
	}

	if release.TagName == "" {
		return nil, fmt.Errorf("no release found on GitHub")
	}

	return &release, nil
}

func findAsset(release *Release) (name string, url string, err error) {
	suffix := fmt.Sprintf("darwin_%s.tar.gz", runtime.GOARCH)
	for _, a := range release.Assets {
		if strings.HasSuffix(a.Name, suffix) {
			return a.Name, a.BrowserDownloadURL, nil
		}
	}
	return "", "", fmt.Errorf("no release asset for darwin/%s", runtime.GOARCH)
}

func findChecksumsURL(release *Release) (string, error) {
	for _, a := range release.Assets {
		if a.Name == "checksums.txt" {
			return a.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("no checksums.txt found in release")
}

func parseChecksums(data []byte, assetName string) (string, error) {
	scanner := func() ([]string, []string) {
		var hashes, names []string
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) == 2 {
				hashes = append(hashes, parts[0])
				names = append(names, parts[1])
			}
		}
		return hashes, names
	}

	hashes, names := scanner()
	for i, name := range names {
		if name == assetName {
			return hashes[i], nil
		}
	}
	return "", fmt.Errorf("checksum for %s not found in checksums.txt", assetName)
}

func DownloadAndInstall(release *Release) error {
	assetName, assetURL, err := findAsset(release)
	if err != nil {
		return err
	}

	checksumsURL, err := findChecksumsURL(release)
	if err != nil {
		return fmt.Errorf("cannot verify release integrity: %w", err)
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find current binary: %w", err)
	}

	fmt.Printf("Downloading %s...\n", release.TagName)

	checksumsClient := &http.Client{Timeout: 30 * time.Second}
	csResp, err := checksumsClient.Get(checksumsURL)
	if err != nil {
		return fmt.Errorf("downloading checksums: %w", err)
	}
	csData, err := io.ReadAll(csResp.Body)
	_ = csResp.Body.Close()
	if err != nil {
		return fmt.Errorf("reading checksums: %w", err)
	}
	if csResp.StatusCode != http.StatusOK {
		return fmt.Errorf("checksums download returned status %d", csResp.StatusCode)
	}

	expectedHash, err := parseChecksums(csData, assetName)
	if err != nil {
		return fmt.Errorf("verifying release checksums: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "llamawizard-update-*.tar.gz")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(assetURL)
	if err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_ = tmpFile.Close()
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmpFile, hasher), resp.Body)
	_ = tmpFile.Close()
	if err != nil {
		return fmt.Errorf("saving download: %w", err)
	}
	if written == 0 {
		return fmt.Errorf("downloaded file is empty")
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch (expected %s, got %s)", expectedHash[:16]+"...", actualHash[:16]+"...")
	}

	binaryPath, err := extractBinary(tmpPath)
	if err != nil {
		return fmt.Errorf("extracting binary: %w", err)
	}
	defer func() { _ = os.Remove(binaryPath) }()

	if err := os.Chmod(binaryPath, 0o755); err != nil {
		return fmt.Errorf("setting permissions: %w", err)
	}

	backupPath := execPath + ".old"
	if err := os.Rename(execPath, backupPath); err != nil {
		return fmt.Errorf("backing up old binary (try sudo?): %w", err)
	}

	if err := os.Rename(binaryPath, execPath); err != nil {
		_ = os.Rename(backupPath, execPath)
		_ = os.Remove(backupPath)
		return fmt.Errorf("installing new binary (try sudo?): %w", err)
	}

	_ = os.Remove(backupPath)
	return nil
}

func extractBinary(tarballPath string) (string, error) {
	f, err := os.Open(tarballPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	gzReader, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("decompressing: %w", err)
	}
	defer func() { _ = gzReader.Close() }()

	tarReader := tar.NewReader(gzReader)

	tmpBinary, err := os.CreateTemp("", "llamawizard-binary-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmpBinary.Name()

	cleanup := func() {
		_ = tmpBinary.Close()
		_ = os.Remove(tmpPath)
	}

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			cleanup()
			return "", fmt.Errorf("reading tar: %w", err)
		}

		base := header.Name
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			base = base[idx+1:]
		}
		if base == "llamawizard" {
			if _, err := io.Copy(tmpBinary, tarReader); err != nil {
				cleanup()
				return "", fmt.Errorf("extracting binary from tar: %w", err)
			}
			_ = tmpBinary.Close()
			return tmpPath, nil
		}
	}

	cleanup()
	return "", fmt.Errorf("binary 'llamawizard' not found in release tarball")
}
