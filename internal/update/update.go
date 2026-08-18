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
	"syscall"
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

// IsNewer reports whether latest is newer than current.
//
// Versions are compared numerically over the first three dot-separated
// parts. A trailing suffix on a part is ignored, so a suffixed version
// compares equal to its base version: a local build made after v0.1.3
// ("v0.1.3-9-g728e74c", "v0.1.3-dirty") is not offered v0.1.3 as an
// update. A pre-release therefore compares equal to the matching stable
// release in either direction: in the *current* direction that is a
// deliberate deviation from semver (such a build is not offered the
// release it previews), and in the *latest* direction it is unreachable
// anyway (GitHub's /releases/latest never serves pre-releases). "dev" —
// unversioned local builds — is always considered older than any release.
func IsNewer(current, latest string) bool {
	if current == "dev" {
		return latest != ""
	}

	a := strings.TrimPrefix(current, "v")
	b := strings.TrimPrefix(latest, "v")

	aParts := padParts(strings.Split(a, "."))
	bParts := padParts(strings.Split(b, "."))

	for i := 0; i < 3; i++ {
		an := leadingInt(aParts[i])
		bn := leadingInt(bParts[i])
		if bn > an {
			return true
		}
		if bn < an {
			return false
		}
	}
	return false
}

// leadingInt parses the leading numeric run of a version part, ignoring any
// suffix: "3" -> 3, "3-9-g728e74c" -> 3, "3-dirty" -> 3, "0-rc1" -> 0,
// "abc" -> 0. This keeps git describe tails and pre-release tags from
// zeroing out the part they attach to.
func leadingInt(s string) int {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	n, _ := strconv.Atoi(s[:i])
	return n
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
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

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
		return nil, ghAPIError(resp, body)
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

// ghAPIError turns a non-200 GitHub API response into an actionable error.
// Rate limits are transient: they get the reset time and, when the call was
// unauthenticated, a pointer at GITHUB_TOKEN — instead of a raw dump of
// GitHub's JSON.
func ghAPIError(resp *http.Response, body []byte) error {
	if !isRateLimited(resp, body) {
		return fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}
	msg := "GitHub API rate limit exceeded"
	if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
		if ts, err := strconv.ParseInt(reset, 10, 64); err == nil {
			if in := time.Until(time.Unix(ts, 0)).Round(time.Minute); in > 0 {
				msg += fmt.Sprintf(", resets in about %s", in)
			}
		}
	}
	if os.Getenv("GITHUB_TOKEN") == "" {
		msg += "; set GITHUB_TOKEN to raise the limit from 60 to 5000 requests/hour"
	}
	return errors.New(msg)
}

func isRateLimited(resp *http.Response, body []byte) bool {
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
		return false
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return true
	}
	return strings.Contains(strings.ToLower(string(body)), "rate limit")
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
	if csResp.StatusCode != http.StatusOK {
		_ = csResp.Body.Close()
		return fmt.Errorf("checksums download returned status %d", csResp.StatusCode)
	}
	csData, err := io.ReadAll(csResp.Body)
	_ = csResp.Body.Close()
	if err != nil {
		return fmt.Errorf("reading checksums: %w", err)
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

	return installBinary(binaryPath, execPath)
}

// installBinary replaces the executable at execPath with the extracted
// binary at binaryPath: the running executable is renamed to
// execPath+".old", the new binary is renamed into its place, and the backup
// is removed.
//
// Renaming (not deleting) a running executable is safe: the kernel keeps
// the old inode mapped for the running process. On darwin os.Executable()
// returns the launch-time path string, which is unaffected by the rename
// and resolves to the new inode after the swap, so a re-exec of that path
// picks up the update.
//
// On a failed install the old binary is restored from the backup. If the
// restore itself fails, the backup is deliberately kept and its path
// reported in the error: removing it would destroy the only remaining copy
// of the old binary.
//
// installBinary does not remove binaryPath; the caller is responsible
// (DownloadAndInstall defers it).
func installBinary(binaryPath, execPath string) error {
	if err := os.Chmod(binaryPath, 0o755); err != nil {
		return fmt.Errorf("setting permissions: %w", err)
	}

	backupPath := execPath + ".old"
	if err := osRename(execPath, backupPath); err != nil {
		return fmt.Errorf("backing up old binary: %w%s", err, renameHint(err))
	}

	if err := osRename(binaryPath, execPath); err != nil {
		restoreErr := osRename(backupPath, execPath)
		if restoreErr == nil {
			return fmt.Errorf("installing new binary: %w%s", err, renameHint(err))
		}
		return fmt.Errorf("installing new binary: %v; restoring the old binary also failed: %v; the old binary is kept at %s", err, restoreErr, backupPath)
	}

	_ = os.Remove(backupPath)
	return nil
}

// osRename is an indirection over os.Rename so tests can simulate specific
// renames failing.
var osRename = os.Rename

// renameHint appends advice for the failure classes that actually happen in
// the self-update renames. "Try sudo" is only right for permission errors;
// a cross-filesystem move (EXDEV) is not fixable with sudo.
func renameHint(err error) string {
	switch {
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return " — the binary's directory is not writable, try running with sudo"
	case errors.Is(err, syscall.EXDEV):
		return " — the temp directory and the binary's location are on different filesystems, self-update cannot move the binary across volumes, install manually from the GitHub release"
	}
	return ""
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
