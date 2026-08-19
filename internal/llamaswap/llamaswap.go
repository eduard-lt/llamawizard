package llamaswap

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/eduard-lt/llamawizard/internal/ctxcalc"
	"github.com/eduard-lt/llamawizard/internal/hardware"
	"github.com/eduard-lt/llamawizard/internal/state"
)

// knownLlamaServerPaths lists well-known install locations for llama-server
// on macOS, ordered by priority.
var knownLlamaServerPaths = []string{}

func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	if home != "" {
		knownLlamaServerPaths = append(knownLlamaServerPaths,
			filepath.Join(home, ".local", "ai", "llama.cpp", "build", "bin", "llama-server"),
		)
	}
	knownLlamaServerPaths = append(knownLlamaServerPaths,
		"/opt/homebrew/bin/llama-server",
		"/usr/local/bin/llama-server",
	)
}

const (
	brewTap     = "mostlygeek/llama-swap"
	githubOwner = "mostlygeek"
	githubRepo  = "llama-swap"
	binaryName  = "llama-swap"
)

var releasesBase = "https://api.github.com/repos/" + githubOwner + "/" + githubRepo + "/releases/latest"

// EnsureLlamaSwap finds an existing llama-swap binary or installs one.
//
// Order of attempts:
//  1. Already on PATH? Verify with --version and return the path.
//  2. Homebrew available? Tap + install via brew.
//  3. GitHub releases fallback — download the darwin prebuilt tarball,
//     extract to a local bin directory, and return the path.
//
// Returns the absolute path to the working llama-swap binary, or an error.
func EnsureLlamaSwap() (string, error) {
	// Step 1: check PATH.
	if path, err := exec.LookPath(binaryName); err == nil {
		if ok, _ := verifyBinary(path); ok {
			return path, nil
		}
	}

	// Step 2: try Homebrew.
	brewPath, brewErr := exec.LookPath("brew")
	if brewErr == nil {
		if path, err := installViaBrew(brewPath); err == nil {
			return path, nil
		}
	}

	// Step 3: GitHub releases fallback.
	return installFromReleases()
}

// ResolveLlamaServerPath locates the llama-server binary using the same
// precedence as the build system, without triggering any build steps.
//
// Order:
//  1. exec.LookPath("llama-server")
//  2. ~/.local/ai/llama.cpp/build/bin/llama-server
//  3. /opt/homebrew/bin/llama-server
//  4. /usr/local/bin/llama-server
//
// Returns the absolute path or an error if not found.
func ResolveLlamaServerPath() (string, error) {
	if p, err := exec.LookPath("llama-server"); err == nil {
		return p, nil
	}

	for _, p := range knownLlamaServerPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("llama-server not found on PATH or at known locations (%s)",
		strings.Join(knownLlamaServerPaths, ", "))
}

// ResolveLlamaSwapPath locates the llama-swap binary without triggering any
// install steps, following the same state-independent convention as
// ResolveLlamaServerPath.
//
// Order:
//  1. exec.LookPath("llama-swap")
//  2. ~/.local/bin/llama-swap (where the GitHub-release installer puts it)
//
// Returns the path or an error if neither exists.
func ResolveLlamaSwapPath() (string, error) {
	if p, err := exec.LookPath(binaryName); err == nil {
		return p, nil
	}

	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".local", "bin", binaryName)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("llama-swap not found on PATH or at ~/.local/bin/%s", binaryName)
}

// verifyBinary runs "<path> --version" and returns true on zero exit.
func verifyBinary(path string) (bool, error) {
	_, err := exec.Command(path, "--version").CombinedOutput()
	return err == nil, err
}

// installViaBrew taps the llama-swap tap and installs it via Homebrew.
func installViaBrew(brewPath string) (string, error) {
	trustCmd := exec.Command(brewPath, "trust", brewTap)
	trustCmd.Stdout = io.Discard
	trustCmd.Stderr = io.Discard
	_ = trustCmd.Run()

	cmd := exec.Command(brewPath, "tap", brewTap)
	var tapStderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &tapStderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("brew tap %s: %w\n%s", brewTap, err, strings.TrimSpace(tapStderr.String()))
	}

	cmd = exec.Command(brewPath, "install", binaryName)
	var installStderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &installStderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("brew install %s: %w\n%s", binaryName, err, strings.TrimSpace(installStderr.String()))
	}

	// Resolve and verify.
	path, err := exec.LookPath(binaryName)
	if err != nil {
		return "", fmt.Errorf("llama-swap not found on PATH after brew install")
	}
	if ok, _ := verifyBinary(path); !ok {
		return "", fmt.Errorf("llama-swap --version failed at %s", path)
	}
	return path, nil
}

// releaseAsset represents a single GitHub release asset.
type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// installFromReleases downloads the latest darwin binary from GitHub releases,
// extracts it to a local bin directory, and returns the path.
func installFromReleases() (string, error) {
	arch := runtime.GOARCH // "arm64" or "amd64"
	filename := fmt.Sprintf("%s_*_darwin_%s.tar.gz", binaryName, arch)

	asset, err := findAsset(filename)
	if err != nil {
		return "", fmt.Errorf("finding llama-swap release asset: %w", err)
	}

	// Determine local bin directory (~/.local/bin).
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	binDir := filepath.Join(home, ".local", "bin")

	tmpFile, cleanup, err := downloadTemp(asset.BrowserDownloadURL, asset.Size)
	if err != nil {
		return "", fmt.Errorf("downloading release: %w", err)
	}
	defer cleanup()

	// Extract the binary from the tarball.
	binPath, err := extractBinary(tmpFile, binDir)
	if err != nil {
		return "", fmt.Errorf("extracting release: %w", err)
	}

	// Verify the extracted binary works.
	if ok, verr := verifyBinary(binPath); !ok {
		return "", fmt.Errorf("llama-swap --version failed after extraction: %v", verr)
	}

	return binPath, nil
}

// findAsset queries GitHub's releases API and returns the matching darwin asset.
func findAsset(pattern string) (*releaseAsset, error) {
	req, err := http.NewRequest("GET", releasesBase, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "llamawizard")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", releasesBase, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release struct {
		Assets []releaseAsset `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decoding release JSON: %w", err)
	}

	for i, a := range release.Assets {
		ok, err := filepath.Match(pattern, a.Name)
		if err != nil {
			continue
		}
		if ok {
			return &release.Assets[i], nil
		}
	}

	var names []string
	for _, a := range release.Assets {
		names = append(names, a.Name)
	}
	return nil, fmt.Errorf("no asset matching %q found; available: %s", pattern, strings.Join(names, ", "))
}

// downloadTemp fetches the URL into a temp file and returns its path plus a cleanup func.
//
// On error, downloadTemp has already released everything it allocated
// (response body, temp file); the returned cleanup is a no-op, safe to
// ignore or call. On success, the caller owns the temp file and must call
// cleanup (e.g. via defer) to remove it.
func downloadTemp(url string, expectedSize int64) (path string, cleanup func(), err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", func() {}, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("User-Agent", "llamawizard")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", func() {}, fmt.Errorf("HTTP GET %s: %w", url, err)
	}
	// Close the body on every path: callers discard the returned cleanup on
	// error, so the function itself must release the response.
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", func() {}, fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	f, err := os.CreateTemp("", "llama-swap-*.tar.gz")
	if err != nil {
		return "", func() {}, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := f.Name()

	n, err := io.Copy(f, resp.Body)
	_ = f.Close()
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", func() {}, fmt.Errorf("writing download: %w", err)
	}

	if expectedSize > 0 && n != expectedSize {
		_ = os.Remove(tmpPath)
		return "", func() {}, fmt.Errorf("size mismatch: expected %d, got %d", expectedSize, n)
	}

	return tmpPath, func() { _ = os.Remove(tmpPath) }, nil
}

// extractBinary unpacks the tarball and places the llama-swap binary into binDir.
func extractBinary(tarballPath, binDir string) (string, error) {
	f, err := os.Open(tarballPath)
	if err != nil {
		return "", fmt.Errorf("opening tarball: %w", err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("gzip open: %w", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("reading tar entry: %w", err)
		}

		base := filepath.Base(header.Name)
		if base != binaryName {
			continue // skip non-binary entries (README, LICENSE, etc.)
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		if err := os.MkdirAll(binDir, 0o755); err != nil {
			return "", fmt.Errorf("creating bin dir %s: %w", binDir, err)
		}

		binPath := filepath.Join(binDir, binaryName)
		out, err := os.OpenFile(binPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return "", fmt.Errorf("creating binary file: %w", err)
		}

		if _, err := io.Copy(out, tr); err != nil {
			_ = out.Close()
			_ = os.Remove(binPath)
			return "", fmt.Errorf("extracting binary: %w", err)
		}
		_ = out.Close()

		return binPath, nil
	}

	return "", fmt.Errorf("no %s binary found in tarball", binaryName)
}

// cmdType is a string that marshals as a YAML literal block scalar (|).
type cmdType string

func (c cmdType) MarshalYAML() (interface{}, error) {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: string(c),
		Style: yaml.LiteralStyle,
	}, nil
}

// modelConfig is the YAML structure for a single llama-swap model entry.
type modelConfig struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Cmd         cmdType  `yaml:"cmd"`
	Aliases     []string `yaml:"aliases,omitempty"`
}

// config is the top-level llama-swap YAML structure.
type config struct {
	ApiKeys *[]string              `yaml:"apiKeys,omitempty"`
	Models  map[string]modelConfig `yaml:"models"`
}

// GenerateConfig builds the llama-swap YAML configuration from model entries.
//
// It produces a YAML document matching the structure of an existing
// ~/.local/ai/config/llama-swap.yaml:
//
//	apiKeys:
//	  - "dummy"
//	models:
//	  "my-model":
//	    name: "My Model"
//	    description: "..."
//	    cmd: |
//	      /path/to/llama-server
//	      --host
//	      127.0.0.1
//	      --port
//	      ${PORT}
//	      --model '/Users/x/models/my-model/model.gguf'
//	      -ngl 999
//	      --ctx-size 32768
//	      --jinja
//	    aliases:
//	      - alias-one
//
// The proxy listen port is not part of this config: it is passed to
// launchd.WritePlist as the -listen flag. ${PORT} is a per-model runtime
// macro that llama-swap allocates from its own startPort range (default
// 5800) — it is not the proxy port.
//
// Model file paths are absolute (~/models/<slug>/<file> expanded) and
// single-quoted: llama-swap does not run a shell, it tokenizes cmd with
// POSIX shlex, so the quotes are the only thing protecting spaces, parens
// and quotes in paths.
// If a model entry has Mmproj set, --mmproj is included in the cmd.
// Sampling params (Temperature, TopK, TopP, RepeatPenalty) are only
// emitted when non-zero. CtxSize defaults to a hardware-aware smart
// calculation when zero — falling back to 32768 if GGUF metadata or
// RAM detection is unavailable.
//
// Two entries mapping to the same model ID (same slug, or the same
// HFRepo when the slug is empty) are rejected with an error: the models
// map is keyed by that ID, so a duplicate would silently drop one model.
func GenerateConfig(models []state.ModelEntry, apiKey string, llamaServerPath string, hw hardware.HardwareInfo) ([]byte, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("getting home directory: %w", err)
	}

	if llamaServerPath == "" {
		resolved, err := ResolveLlamaServerPath()
		if err != nil {
			return nil, fmt.Errorf("cannot generate config: llama-server binary not found: %w", err)
		}
		llamaServerPath = resolved
	}

	calc := ctxcalc.New(hw)

	cfg := config{
		Models: make(map[string]modelConfig, len(models)),
	}

	if apiKey != "" {
		cfg.ApiKeys = &[]string{apiKey}
	}

	// seen maps each config key to the index of the first entry that uses
	// it. The YAML models map is keyed by this ID, so a second entry with
	// the same key would silently overwrite the first — reject instead.
	seen := make(map[string]int, len(models))

	for i, m := range models {
		modelID := m.Slug
		if modelID == "" {
			modelID = m.HFRepo
		}

		if first, dup := seen[modelID]; dup {
			return nil, fmt.Errorf("models[%d] and models[%d] share the llama-swap model ID %q (slugs %q and %q): the later entry would be silently dropped from the config — remove or rename one of them",
				first, i, modelID, models[first].Slug, m.Slug)
		}
		seen[modelID] = i

		name := m.Name
		if name == "" {
			name = titleCase(strings.ReplaceAll(modelID, "-", " "))
		}

		desc := m.Description
		if desc == "" {
			desc = fmt.Sprintf("%s %s", m.Quant, name)
		}

		modelPath := filepath.Join(home, "models", m.Slug, m.File)

		ctxSize := m.CtxSize
		if ctxSize == 0 {
			r := calc.CalcSingle(modelPath, m.SizeBytes, true)
			if !r.UsedDefault {
				ctxSize = r.CtxSize
			} else {
				ctxSize = ctxcalc.DefaultCtxSize
			}
		}

		var args []string
		args = append(args,
			shellQuote(llamaServerPath),
			"--host", "127.0.0.1",
			"--port", "${PORT}",
			fmt.Sprintf("--model %s", shellQuote(modelPath)),
		)
		if m.Mmproj != "" {
			mmprojPath := filepath.Join(home, "models", m.Slug, m.Mmproj)
			args = append(args, fmt.Sprintf("--mmproj %s", shellQuote(mmprojPath)))
		}
		args = append(args,
			"-ngl 999",
			fmt.Sprintf("--ctx-size %d", ctxSize),
			"--jinja",
		)
		if m.Temperature != 0 {
			args = append(args, fmt.Sprintf("--temp %.2f", m.Temperature))
		}
		if m.TopK != 0 {
			args = append(args, fmt.Sprintf("--top-k %d", m.TopK))
		}
		if m.TopP != 0 {
			args = append(args, fmt.Sprintf("--top-p %.2f", m.TopP))
		}
		if m.RepeatPenalty != 0 {
			args = append(args, fmt.Sprintf("--repeat-penalty %.2f", m.RepeatPenalty))
		}

		cfg.Models[modelID] = modelConfig{
			Name:        name,
			Description: desc,
			Cmd:         cmdType(strings.Join(args, "\n") + "\n"),
			Aliases:     m.Aliases,
		}
	}

	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshalling config: %w", err)
	}

	if err := ValidateConfigYAML(yamlBytes); err != nil {
		return nil, fmt.Errorf("generated config is invalid: %w", err)
	}

	return yamlBytes, nil
}

// shellQuote wraps s in single quotes so llama-swap's shlex-based cmd
// tokenizer keeps it as a single argument. llama-swap does not run a
// shell: it splits cmd with POSIX shlex, so an unquoted space or
// parenthesis would split the path into multiple arguments, and an
// unquoted quote character would be stripped silently. A literal single
// quote is escaped the standard shell way — close the quote, emit a
// backslash-escaped quote, reopen the quote — which shlex re-joins into
// the original character.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// titleCase capitalizes the first letter of each word.
func titleCase(s string) string {
	return strings.TrimSpace(func(r []rune) string {
		if len(r) == 0 {
			return ""
		}
		r[0] = unicode.ToUpper(r[0])
		for i := 1; i < len(r); i++ {
			if r[i-1] == ' ' {
				r[i] = unicode.ToUpper(r[i])
			}
		}
		return string(r)
	}([]rune(s)))
}

// WriteConfig writes newContent to path, but only if the file doesn't exist
// or its content differs.
//
// If the file exists and is identical, returns changed=false immediately.
// If the file exists and differs, returns changed=false along with a
// line-by-line diff (unified-style +/- prefixes, no hunk headers — it is for
// display, not for patching) — it does NOT overwrite. The caller (wizard)
// should present the diff to the user and call ForceWrite if they confirm.
//
// On first run (file doesn't exist), writes cleanly and returns changed=true.
func WriteConfig(path string, newContent []byte) (changed bool, diff string, err error) {
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// First run — write cleanly.
			return true, "", forceWrite(path, newContent)
		}
		return false, "", fmt.Errorf("reading existing config: %w", err)
	}

	// Content matches — nothing to do.
	if bytes.Equal(existing, newContent) {
		return false, "", nil
	}

	// Content differs — return diff without writing.
	diffText := computeDiff(path, existing, newContent)
	return false, diffText, nil
}

// ForceWrite unconditionally writes newContent to path, creating parent
// directories as needed. Call this after WriteConfig returns a diff and
// the user confirms the overwrite.
func ForceWrite(path string, newContent []byte) error {
	return forceWrite(path, newContent)
}

// forceWrite is the internal write helper used by both WriteConfig (first
// run) and ForceWrite (confirmed overwrite).
func forceWrite(path string, data []byte) error {
	if err := ValidateConfigYAML(data); err != nil {
		return fmt.Errorf("refusing to write invalid config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating parent directories for %s: %w", path, err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming to final path: %w", err)
	}
	return nil
}

// ValidateConfigYAML parses the given YAML bytes as a llama-swap config and
// verifies that every model's cmd block starts with a valid executable path.
// It also ensures the YAML is syntactically valid and uses consistent
// whitespace (no tabs).
func ValidateConfigYAML(data []byte) error {
	if bytes.Contains(data, []byte{'\t'}) {
		return fmt.Errorf("config contains tab characters; use spaces for indentation")
	}

	var cfg config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}

	for modelID, m := range cfg.Models {
		cmd := string(m.Cmd)
		cmdLines := strings.Split(cmd, "\n")

		firstLine := ""
		for _, line := range cmdLines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				firstLine = trimmed
				break
			}
		}

		if firstLine == "" {
			return fmt.Errorf("model %q has an empty cmd block", modelID)
		}

		if strings.HasPrefix(firstLine, "-") {
			return fmt.Errorf("model %q cmd block starts with flag %q instead of an executable path", modelID, firstLine)
		}
	}

	return nil
}

// computeDiff produces a unified-style, line-by-line diff between old and new
// content: a ---/+++ header followed by each line prefixed with ' ', '+' or
// '-'. No hunk headers or line numbers — the wizard renders it as-is.
func computeDiff(filename string, old, new []byte) string {
	linesOld := splitLines(old)
	linesNew := splitLines(new)
	diffs := myDiff(linesOld, linesNew)

	var buf strings.Builder
	_, _ = fmt.Fprintf(&buf, "--- %s\n+++ %s (proposed)\n", filename, filename)
	for _, d := range diffs {
		buf.WriteByte(d.kind)
		buf.WriteString(d.text)
		if !strings.HasSuffix(d.text, "\n") {
			buf.WriteByte('\n')
		}
	}
	return buf.String()
}

// diffLine represents a single unified-diff line.
type diffLine struct {
	kind byte // ' ', '+', '-'
	text string
}

// myDiff computes a simple LCS-based diff of two line slices and returns
// unified-diff style entries.
func myDiff(old, new []string) []diffLine {
	m, n := len(old), len(new)

	// Build LCS table.
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if old[i-1] == new[j-1] {
				lcs[i][j] = lcs[i-1][j-1] + 1
			} else if lcs[i-1][j] > lcs[i][j-1] {
				lcs[i][j] = lcs[i-1][j]
			} else {
				lcs[i][j] = lcs[i][j-1]
			}
		}
	}

	// Back-track to produce diff.
	var result []diffLine
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && old[i-1] == new[j-1] {
			result = append(result, diffLine{' ', old[i-1] + "\n"})
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			result = append(result, diffLine{'+', new[j-1] + "\n"})
			j--
		} else {
			result = append(result, diffLine{'-', old[i-1] + "\n"})
			i--
		}
	}

	// Reverse to get forward order.
	for a, b := 0, len(result)-1; a < b; a, b = a+1, b-1 {
		result[a], result[b] = result[b], result[a]
	}
	return result
}

func splitLines(data []byte) []string {
	lines := strings.Split(string(data), "\n")
	// Remove trailing empty element from final newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
