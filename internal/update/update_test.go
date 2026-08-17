package update

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.1.0", false},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0", "v1.0.1", true},
		{"v0.9.9", "v1.0.0", true},
		{"v2.0.0", "v1.9.9", false},
		{"0.1.0", "0.2.0", true},
		{"dev", "v0.1.0", true},
		{"dev", "", false},
		{"v1.0", "v1.0.1", true},
		{"v1.0.0", "v1.0", false},
		// Two-digit patch ordering (real tags in this repo: v0.3.2, v0.3.10).
		{"v0.3.2", "v0.3.10", true},
		// Empty current parses as 0.0.0.
		{"", "v0.0.1", true},
	}

	for _, tc := range tests {
		got := IsNewer(tc.current, tc.latest)
		if got != tc.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestIsNewerSuffixedVersions(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    bool
	}{
		// git describe tails (Taskfile local builds): the patch part must
		// keep its numeric prefix instead of zeroing out, so a local build
		// made after a release is not offered that release as an update.
		{"v0.1.3-9-g728e74c", "v0.1.3", false},
		{"v0.1.3-dirty", "v0.1.3", false},
		{"v0.1.3-9-g728e74c", "v0.1.4", true},
		// Go pseudo-version build (go build of a dirty module), genuinely older.
		{"v0.1.1-0.20260812140328-5e88d3d7c84d+dirty", "v0.1.3", true},
		// Bare short hash (git describe --always with no tags): parses as 0.0.0.
		{"abc1234", "v0.1.0", true},
		// Pre-release suffixes compare as their base version (deliberate
		// simplification; /releases/latest never serves pre-releases).
		{"0.1.5-beta", "0.1.5", false},
		{"0.2.0-rc1", "0.2.0", false},
		// A pre-release of a higher version is still ahead of an older stable.
		{"0.1.9", "0.2.0-rc1", true},
		// A stable is not older than a pre-release of the same version.
		{"0.2.0", "0.2.0-alpha", false},
	}

	for _, tc := range tests {
		got := IsNewer(tc.current, tc.latest)
		if got != tc.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestLeadingInt(t *testing.T) {
	tests := map[string]int{
		"0":            0,
		"12":           12,
		"3-9-g728e74c": 3,
		"3-dirty":      3,
		"0-rc1":        0,
		"abc":          0,
		"":             0,
	}
	for in, want := range tests {
		if got := leadingInt(in); got != want {
			t.Errorf("leadingInt(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestPadParts(t *testing.T) {
	tests := []struct {
		input []string
		want  []string
	}{
		{[]string{"1"}, []string{"1", "0", "0"}},
		{[]string{"1", "2"}, []string{"1", "2", "0"}},
		{[]string{"1", "2", "3"}, []string{"1", "2", "3"}},
	}

	for _, tc := range tests {
		got := padParts(tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("padParts(%v) len = %d, want %d", tc.input, len(got), len(tc.want))
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("padParts(%v)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
			}
		}
	}
}

func TestCheckLatest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/releases/latest") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Release{
			TagName: "v0.2.0",
			Assets: []Asset{
				{Name: fmt.Sprintf("llamawizard_v0.2.0_darwin_%s.tar.gz", runtime.GOARCH), BrowserDownloadURL: "http://example.com/tarball.tar.gz", Size: 1024},
				{Name: "checksums.txt", BrowserDownloadURL: "http://example.com/checksums.txt", Size: 128},
			},
		})
	}))
	defer ts.Close()

	prev := apiBaseURL
	apiBaseURL = ts.URL
	defer func() { apiBaseURL = prev }()

	release, err := CheckLatest()
	if err != nil {
		t.Fatalf("CheckLatest() error = %v", err)
	}
	if release.TagName != "v0.2.0" {
		t.Errorf("TagName = %q, want %q", release.TagName, "v0.2.0")
	}
	if len(release.Assets) != 2 {
		t.Errorf("got %d assets, want 2", len(release.Assets))
	}
}

func TestFindAsset(t *testing.T) {
	release := &Release{
		TagName: "v0.1.0",
		Assets: []Asset{
			{Name: fmt.Sprintf("llamawizard_v0.1.0_darwin_%s.tar.gz", runtime.GOARCH), BrowserDownloadURL: "http://example.com/tarball.tar.gz"},
			{Name: "checksums.txt", BrowserDownloadURL: "http://example.com/checksums.txt"},
		},
	}

	name, url, err := findAsset(release)
	if err != nil {
		t.Fatalf("findAsset() error = %v", err)
	}
	expectedSuffix := fmt.Sprintf("darwin_%s.tar.gz", runtime.GOARCH)
	if !strings.HasSuffix(name, expectedSuffix) {
		t.Errorf("name = %q, want suffix %q", name, expectedSuffix)
	}
	if url != "http://example.com/tarball.tar.gz" {
		t.Errorf("url = %q, want %q", url, "http://example.com/tarball.tar.gz")
	}
}

func TestFindAssetNotFound(t *testing.T) {
	release := &Release{
		TagName: "v0.1.0",
		Assets: []Asset{
			{Name: "checksums.txt", BrowserDownloadURL: "http://example.com/checksums.txt"},
		},
	}

	_, _, err := findAsset(release)
	if err == nil {
		t.Fatal("expected error for missing asset")
	}
}

func TestParseChecksums(t *testing.T) {
	content := `abc123def456  llamawizard_v0.1.0_darwin_arm64.tar.gz
789012abc345  llamawizard_v0.1.0_darwin_amd64.tar.gz
`

	hash, err := parseChecksums([]byte(content), "llamawizard_v0.1.0_darwin_arm64.tar.gz")
	if err != nil {
		t.Fatalf("parseChecksums() error = %v", err)
	}
	if hash != "abc123def456" {
		t.Errorf("hash = %q, want %q", hash, "abc123def456")
	}
}

func TestParseChecksumsNotFound(t *testing.T) {
	content := `abc123def456  other_file.tar.gz
`

	_, err := parseChecksums([]byte(content), "my_file.tar.gz")
	if err == nil {
		t.Fatal("expected error for missing checksum")
	}
}

func TestDownloadAndInstall(t *testing.T) {
	tarballContent, tarballHash := createTestTarball(t)
	tarballName := fmt.Sprintf("llamawizard_v0.1.0_darwin_%s.tar.gz", runtime.GOARCH)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "checksums"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = fmt.Fprintf(w, "%s  %s\n", tarballHash, tarballName)
		case strings.Contains(r.URL.Path, "tarball"):
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarballContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	release := &Release{
		TagName: "v0.1.0",
		Assets: []Asset{
			{Name: tarballName, BrowserDownloadURL: ts.URL + "/tarball"},
			{Name: "checksums.txt", BrowserDownloadURL: ts.URL + "/checksums"},
		},
	}

	err := DownloadAndInstall(release)
	if err != nil {
		t.Fatalf("DownloadAndInstall() error = %v", err)
	}
}

func TestDownloadAndInstallChecksumMismatch(t *testing.T) {
	tarballContent, _ := createTestTarball(t)
	tarballName := fmt.Sprintf("llamawizard_v0.1.0_darwin_%s.tar.gz", runtime.GOARCH)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "checksums"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = fmt.Fprintf(w, "deadbeefdeadbeefdeadbeefdeadbeef00000000  %s\n", tarballName)
		case strings.Contains(r.URL.Path, "tarball"):
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarballContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	release := &Release{
		TagName: "v0.1.0",
		Assets: []Asset{
			{Name: tarballName, BrowserDownloadURL: ts.URL + "/tarball"},
			{Name: "checksums.txt", BrowserDownloadURL: ts.URL + "/checksums"},
		},
	}

	err := DownloadAndInstall(release)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error = %v, want checksum mismatch", err)
	}
}

func TestDownloadAndInstallMissingChecksums(t *testing.T) {
	tarballName := fmt.Sprintf("llamawizard_v0.1.0_darwin_%s.tar.gz", runtime.GOARCH)

	release := &Release{
		TagName: "v0.1.0",
		Assets: []Asset{
			{Name: tarballName, BrowserDownloadURL: "http://example.com/tarball"},
		},
	}

	err := DownloadAndInstall(release)
	if err == nil {
		t.Fatal("expected error for missing checksums, got nil")
	}
}

func TestExtractBinary(t *testing.T) {
	tarballContent, _ := createTestTarball(t)

	tmpDir := t.TempDir()
	tarballPath := filepath.Join(tmpDir, "test.tar.gz")

	if err := os.WriteFile(tarballPath, tarballContent, 0o644); err != nil {
		t.Fatalf("writing temp tarball: %v", err)
	}

	binaryPath, err := extractBinary(tarballPath)
	if err != nil {
		t.Fatalf("extractBinary() error = %v", err)
	}
	defer func() { _ = os.Remove(binaryPath) }()

	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("reading extracted binary: %v", err)
	}
	if string(data) != "llamawizard binary content" {
		t.Errorf("extracted content = %q, want %q", string(data), "llamawizard binary content")
	}
}

func createTestTarball(t *testing.T) ([]byte, string) {
	t.Helper()

	tmpDir := t.TempDir()
	content := "llamawizard binary content"

	tarballPath := filepath.Join(tmpDir, "test.tar.gz")
	f, err := os.Create(tarballPath)
	if err != nil {
		t.Fatalf("creating tarball file: %v", err)
	}

	gzWriter := gzip.NewWriter(f)
	tarWriter := tar.NewWriter(gzWriter)

	if err := tarWriter.WriteHeader(&tar.Header{
		Name: "llamawizard_v0.1.0/llamawizard",
		Size: int64(len(content)),
		Mode: 0o755,
	}); err != nil {
		t.Fatalf("writing tar header: %v", err)
	}
	if _, err := tarWriter.Write([]byte(content)); err != nil {
		t.Fatalf("writing tar body: %v", err)
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatalf("closing tar: %v", err)
	}
	if err := gzWriter.Close(); err != nil {
		t.Fatalf("closing gzip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("closing file: %v", err)
	}

	data, err := os.ReadFile(tarballPath)
	if err != nil {
		t.Fatalf("reading tarball: %v", err)
	}

	hasher := sha256.New()
	hasher.Write(data)
	tarballHash := hex.EncodeToString(hasher.Sum(nil))

	return data, tarballHash
}

func TestDownloadAndInstallEmptyDownload(t *testing.T) {
	tarballName := fmt.Sprintf("llamawizard_v0.1.0_darwin_%s.tar.gz", runtime.GOARCH)

	// Create an empty tarball but a valid checksum of empty content
	emptyTar, err := createEmptyTarball(t)
	if err != nil {
		t.Fatal(err)
	}

	hasher := sha256.New()
	hasher.Write(emptyTar)
	emptyHash := hex.EncodeToString(hasher.Sum(nil))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "checksums"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = fmt.Fprintf(w, "%s  %s\n", emptyHash, tarballName)
		case strings.Contains(r.URL.Path, "tarball"):
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(emptyTar)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	release := &Release{
		TagName: "v0.1.0",
		Assets: []Asset{
			{Name: tarballName, BrowserDownloadURL: ts.URL + "/tarball"},
			{Name: "checksums.txt", BrowserDownloadURL: ts.URL + "/checksums"},
		},
	}

	err = DownloadAndInstall(release)
	if err == nil {
		t.Fatal("expected error for zero-size download, got nil")
	}
}

func createEmptyTarball(t *testing.T) ([]byte, error) {
	t.Helper()
	tmpDir := t.TempDir()
	tarballPath := filepath.Join(tmpDir, "empty.tar.gz")
	f, err := os.Create(tarballPath)
	if err != nil {
		return nil, err
	}
	gzWriter := gzip.NewWriter(f)
	tarWriter := tar.NewWriter(gzWriter)

	if err := tarWriter.WriteHeader(&tar.Header{
		Name: "llamawizard",
		Size: 10,
		Mode: 0o755,
	}); err != nil {
		return nil, err
	}

	_ = tarWriter.Close()
	_ = gzWriter.Close()
	_ = f.Close()

	return os.ReadFile(tarballPath)
}

func TestCheckLatestNoAssets(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/releases/latest") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Release{
			TagName: "v0.1.0",
			Assets:  []Asset{},
		})
	}))
	defer ts.Close()

	prev := apiBaseURL
	apiBaseURL = ts.URL
	defer func() { apiBaseURL = prev }()

	release, err := CheckLatest()
	if err != nil {
		t.Fatalf("CheckLatest() error = %v", err)
	}
	if release.TagName != "v0.1.0" {
		t.Errorf("TagName = %q, want %q", release.TagName, "v0.1.0")
	}
}

func TestParseChecksumsExtraWhitespace(t *testing.T) {
	content := "\n  abc123  file.tar.gz  \n\n  def456  other.txt\n\n"

	hash, err := parseChecksums([]byte(content), "other.txt")
	if err != nil {
		t.Fatalf("parseChecksums() error = %v", err)
	}
	if hash != "def456" {
		t.Errorf("hash = %q, want %q", hash, "def456")
	}
}

func TestDownloadAndInstallBadStatus(t *testing.T) {
	tarballName := fmt.Sprintf("llamawizard_v0.1.0_darwin_%s.tar.gz", runtime.GOARCH)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "checksums") {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = fmt.Fprintf(w, "abc123  %s\n", tarballName)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer ts.Close()

	release := &Release{
		TagName: "v0.1.0",
		Assets: []Asset{
			{Name: tarballName, BrowserDownloadURL: ts.URL + "/tarball"},
			{Name: "checksums.txt", BrowserDownloadURL: ts.URL + "/checksums"},
		},
	}

	err := DownloadAndInstall(release)
	if err == nil {
		t.Fatal("expected error for 500 status, got nil")
	}
}
