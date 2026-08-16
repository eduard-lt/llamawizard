package llamaswap

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/eduard-lt/llamawizard/internal/hardware"
	"github.com/eduard-lt/llamawizard/internal/state"
)

// --- B4: EnsureLlamaSwap tests ---

func TestEnsureLlamaSwap_FindsExisting(t *testing.T) {
	path, err := EnsureLlamaSwap()
	if err != nil {
		t.Fatalf("EnsureLlamaSwap failed: %v", err)
	}
	if path == "" {
		t.Fatal("binary path should not be empty")
	}
	if !strings.HasSuffix(path, binaryName) {
		t.Errorf("path should end with %q, got %s", binaryName, path)
	}

	// Confirm it actually works.
	out, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("llama-swap --version failed: %v (%s)", err, out)
	}
	t.Logf("found llama-swap at %s: %s", path, strings.TrimSpace(string(out)))
}

func TestVerifyBinary_Existing(t *testing.T) {
	path, err := exec.LookPath(binaryName)
	if err != nil {
		t.Skipf("%s not found on PATH", binaryName)
	}
	ok, err := verifyBinary(path)
	if !ok {
		t.Errorf("verifyBinary(%q) = %v, %v; want true, nil", path, ok, err)
	}
}

func TestVerifyBinary_Nonexistent(t *testing.T) {
	ok, err := verifyBinary("/nonexistent/path/to/llama-swap")
	if ok {
		t.Error("verifyBinary should return false for nonexistent path")
	}
	if err == nil {
		t.Error("verifyBinary should return error for nonexistent path")
	}
}

func TestFindAsset(t *testing.T) {
	arch := getRuntimeArch()
	if arch == "" {
		t.Skip("could not determine GOARCH")
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"assets": [{"name": "llama-swap_1.2.3_darwin_%s.tar.gz", "browser_download_url": "http://example.com/asset.tar.gz", "size": 12345}]}`, arch)
	}))
	defer ts.Close()

	prev := releasesBase
	releasesBase = ts.URL
	defer func() { releasesBase = prev }()

	pattern := fmt.Sprintf("%s_*_darwin_%s.tar.gz", binaryName, arch)

	asset, err := findAsset(pattern)
	if err != nil {
		t.Fatalf("findAsset(%q) failed: %v", pattern, err)
	}
	if asset == nil {
		t.Fatal("findAsset returned nil")
	}
	if !strings.Contains(asset.Name, "darwin") {
		t.Errorf("asset name should contain 'darwin': %s", asset.Name)
	}
	if asset.BrowserDownloadURL == "" {
		t.Error("asset download URL should not be empty")
	}
	if asset.Size <= 0 {
		t.Errorf("asset size should be positive, got %d", asset.Size)
	}
	t.Logf("found asset: %s (%d bytes)", asset.Name, asset.Size)
}

func TestFindAsset_WrongArch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"assets": [{"name": "llama-swap_1.2.3_darwin_arm64.tar.gz", "browser_download_url": "http://example.com/asset.tar.gz", "size": 12345}]}`))
	}))
	defer ts.Close()

	prev := releasesBase
	releasesBase = ts.URL
	defer func() { releasesBase = prev }()

	pattern := fmt.Sprintf("%s_*_darwin_freebee.tar.gz", binaryName)
	_, err := findAsset(pattern)
	if err == nil {
		t.Error("findAsset should fail for nonexistent arch")
	}
}

func TestExtractBinary(t *testing.T) {
	tmpDir := t.TempDir()
	tarballPath := filepath.Join(tmpDir, "test.tar.gz")

	f, err := os.Create(tarballPath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	const fakeContent = "FAKE_LLAMA_SWAP_BINARY"
	hdr := &tar.Header{
		Name:     "llama-swap",
		Mode:     0o755,
		Size:     int64(len(fakeContent)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(fakeContent)); err != nil {
		t.Fatal(err)
	}

	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()

	binDir := filepath.Join(tmpDir, "bin")
	binPath, err := extractBinary(tarballPath, binDir)
	if err != nil {
		t.Fatalf("extractBinary failed: %v", err)
	}
	expectedPath := filepath.Join(binDir, binaryName)
	if binPath != expectedPath {
		t.Errorf("bin path = %q, want %q", binPath, expectedPath)
	}

	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != fakeContent {
		t.Errorf("content = %q, want %q", string(data), fakeContent)
	}
}

func TestExtractBinary_NoMatchingFile(t *testing.T) {
	tmpDir := t.TempDir()
	tarballPath := filepath.Join(tmpDir, "test.tar.gz")

	f, err := os.Create(tarballPath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	hdr := &tar.Header{
		Name:     "other-file",
		Mode:     0o644,
		Size:     5,
		Typeflag: tar.TypeReg,
	}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte("hello"))

	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()

	binDir := filepath.Join(tmpDir, "bin")
	_, err = extractBinary(tarballPath, binDir)
	if err == nil {
		t.Error("extractBinary should fail when no llama-swap binary is in tarball")
	}
}

func TestDownloadTemp_HappyPath(t *testing.T) {
	const body = "test download content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		_, _ = fmt.Fprintf(w, "%s", body)
	}))
	defer srv.Close()

	path, cleanup, err := downloadTemp(srv.URL, int64(len(body)))
	if err != nil {
		t.Fatalf("downloadTemp failed: %v", err)
	}
	defer cleanup()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != body {
		t.Errorf("content = %q, want %q", string(data), body)
	}
}

func TestDownloadTemp_SizeMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()

	_, cleanup, err := downloadTemp(srv.URL, 999)
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Error("downloadTemp should fail on size mismatch")
	}
}

func TestDownloadTemp_HttpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, cleanup, err := downloadTemp(srv.URL, 0)
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Error("downloadTemp should fail on non-200 response")
	}
}

// --- B5: GenerateConfig tests ---

func TestGenerateConfig_Basic(t *testing.T) {
	models := []state.ModelEntry{
		{
			Slug:  "gemma4-chat",
			Name:  "Gemma 4 26B (Chat)",
			Quant: "Q6_K",
			File:  "google_gemma-4-26B-A4B-it-Q6_K.gguf",
		},
	}

	data, err := GenerateConfig(models, "dummy", "/opt/homebrew/bin/llama-server", hardware.HardwareInfo{})
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	yamlStr := string(data)

	// Must have top-level keys.
	if !strings.Contains(yamlStr, "apiKeys:") {
		t.Error("missing apiKeys key")
	}
	if !strings.Contains(yamlStr, "models:") {
		t.Error("missing models key")
	}
	if !strings.Contains(yamlStr, "- dummy") {
		t.Error("missing api key value")
	}

	// Model entry.
	if !strings.Contains(yamlStr, "gemma4-chat:") {
		t.Error("missing model ID key")
	}
	if !strings.Contains(yamlStr, "Gemma 4 26B (Chat)") {
		t.Error("missing model name")
	}

	// Cmd block must be literal block scalar.
	if !strings.Contains(yamlStr, "cmd: |") {
		t.Error("cmd should use literal block scalar (|)")
	}
	if !strings.Contains(yamlStr, "/opt/homebrew/bin/llama-server") {
		t.Error("cmd should contain llama-server path")
	}
	if !strings.Contains(yamlStr, "--host") || !strings.Contains(yamlStr, "127.0.0.1") {
		t.Error("cmd should contain --host 127.0.0.1")
	}
	if !strings.Contains(yamlStr, "--port") || !strings.Contains(yamlStr, "${PORT}") {
		t.Error("cmd should contain --port ${PORT}")
	}
	if !strings.Contains(yamlStr, "-ngl 999") {
		t.Error("cmd should contain -ngl 999")
	}
	if !strings.Contains(yamlStr, "--ctx-size 32768") {
		t.Error("cmd should contain default ctx-size 32768")
	}
	if !strings.Contains(yamlStr, "--jinja") {
		t.Error("cmd should contain --jinja")
	}

	t.Logf("generated YAML:\n%s", yamlStr)
}

func TestGenerateConfig_Mmproj(t *testing.T) {
	models := []state.ModelEntry{
		{
			Slug:   "vision-model",
			Name:   "Vision Model",
			Quant:  "Q4_K_M",
			File:   "model.gguf",
			Mmproj: "mmproj.gguf",
		},
	}

	data, err := GenerateConfig(models, "key123", "/usr/local/bin/llama-server", hardware.HardwareInfo{})
	if err != nil {
		t.Fatal(err)
	}

	yamlStr := string(data)
	if !strings.Contains(yamlStr, "--mmproj") {
		t.Error("cmd should contain --mmproj when Mmproj is set")
	}
	if !strings.Contains(yamlStr, "mmproj.gguf") {
		t.Error("cmd should contain mmproj filename")
	}
}

func TestGenerateConfig_SamplingParams(t *testing.T) {
	models := []state.ModelEntry{
		{
			Slug:          "heavy-model",
			Name:          "Heavy Model",
			Quant:         "Q8_0",
			File:          "model.gguf",
			CtxSize:       65536,
			Temperature:   0.7,
			TopK:          20,
			TopP:          0.8,
			RepeatPenalty: 1.05,
		},
	}

	data, err := GenerateConfig(models, "dummy", "/path/to/llama-server", hardware.HardwareInfo{})
	if err != nil {
		t.Fatal(err)
	}

	yamlStr := string(data)
	checkContains := func(expected string) {
		if !strings.Contains(yamlStr, expected) {
			t.Errorf("missing %q in generated YAML:\n%s", expected, yamlStr)
		}
	}
	checkContains("--ctx-size 65536")
	checkContains("--temp 0.70")
	checkContains("--top-k 20")
	checkContains("--top-p 0.80")
	checkContains("--repeat-penalty 1.05")
}

func TestGenerateConfig_NoSamplingParams(t *testing.T) {
	models := []state.ModelEntry{
		{
			Slug:  "simple-model",
			Name:  "Simple Model",
			Quant: "Q4_K_M",
			File:  "model.gguf",
		},
	}

	data, err := GenerateConfig(models, "dummy", "/path/to/llama-server", hardware.HardwareInfo{})
	if err != nil {
		t.Fatal(err)
	}

	yamlStr := string(data)
	checkNotContains := func(s string) {
		if strings.Contains(yamlStr, s) {
			t.Errorf("should not contain %q:\n%s", s, yamlStr)
		}
	}
	checkNotContains("--temp")
	checkNotContains("--top-k")
	checkNotContains("--top-p")
	checkNotContains("--repeat-penalty")
}

func TestGenerateConfig_Aliases(t *testing.T) {
	models := []state.ModelEntry{
		{
			Slug:  "claude-proxy",
			Name:  "Claude Proxy",
			Quant: "Q4_K_M",
			File:  "model.gguf",
			Aliases: []string{
				"claude-sonnet-4-5",
				"claude-opus-4-5",
			},
		},
	}

	data, err := GenerateConfig(models, "dummy", "/path/to/llama-server", hardware.HardwareInfo{})
	if err != nil {
		t.Fatal(err)
	}

	yamlStr := string(data)
	if !strings.Contains(yamlStr, "aliases:") {
		t.Error("missing aliases key")
	}
	if !strings.Contains(yamlStr, "- claude-sonnet-4-5") {
		t.Error("missing first alias")
	}
	if !strings.Contains(yamlStr, "- claude-opus-4-5") {
		t.Error("missing second alias")
	}
}

func TestGenerateConfig_MultipleModels(t *testing.T) {
	models := []state.ModelEntry{
		{Slug: "model-a", Name: "Model A", Quant: "Q4", File: "a.gguf"},
		{Slug: "model-b", Name: "Model B", Quant: "Q8", File: "b.gguf"},
		{Slug: "model-c", Name: "Model C", Quant: "Q6", File: "c.gguf"},
	}

	data, err := GenerateConfig(models, "secret", "/srv/llama-server", hardware.HardwareInfo{})
	if err != nil {
		t.Fatal(err)
	}

	yamlStr := string(data)
	for _, slug := range []string{"model-a:", "model-b:", "model-c:"} {
		if !strings.Contains(yamlStr, slug) {
			t.Errorf("missing model key %s", slug)
		}
	}
}

func TestGenerateConfig_DuplicateSlug(t *testing.T) {
	models := []state.ModelEntry{
		{Slug: "model-a", Name: "Model A", Quant: "Q4_K_M", File: "a1.gguf"},
		{Slug: "model-a", Name: "Model A (2)", Quant: "Q4_K_M", File: "a2.gguf"},
	}

	_, err := GenerateConfig(models, "dummy", "/opt/homebrew/bin/llama-server", hardware.HardwareInfo{})
	if err == nil {
		t.Fatal("expected error for duplicate slug, got nil")
	}
	if !strings.Contains(err.Error(), "model-a") {
		t.Errorf("error should name the colliding model ID, got: %v", err)
	}
}

func TestGenerateConfig_DuplicateHFRepoFallback(t *testing.T) {
	models := []state.ModelEntry{
		{Slug: "", HFRepo: "org/repo", Quant: "Q4_K_M", File: "a.gguf"},
		{Slug: "", HFRepo: "org/repo", Quant: "Q4_K_M", File: "b.gguf"},
	}

	_, err := GenerateConfig(models, "dummy", "/opt/homebrew/bin/llama-server", hardware.HardwareInfo{})
	if err == nil {
		t.Fatal("expected error for duplicate HFRepo fallback key, got nil")
	}
	if !strings.Contains(err.Error(), "org/repo") {
		t.Errorf("error should name the colliding model ID, got: %v", err)
	}
}

func TestGenerateConfig_DefaultNameAndDescription(t *testing.T) {
	models := []state.ModelEntry{
		{Slug: "my-cool-model", Quant: "Q4_K_M", File: "model.gguf"},
	}

	data, err := GenerateConfig(models, "dummy", "/path/to/llama-server", hardware.HardwareInfo{})
	if err != nil {
		t.Fatal(err)
	}

	yamlStr := string(data)
	// Name should be derived from slug with spaces and title case.
	if !strings.Contains(yamlStr, "My Cool Model") {
		t.Errorf("expected derived name 'My Cool Model', got:\n%s", yamlStr)
	}
	// Description should include quant.
	if !strings.Contains(yamlStr, "Q4_K_M") {
		t.Error("description should contain quant string")
	}
}

func TestGenerateConfig_CustomDescription(t *testing.T) {
	models := []state.ModelEntry{
		{
			Slug:        "my-model",
			Name:        "My Model",
			Description: "Custom description here",
			Quant:       "Q4_K_M",
			File:        "model.gguf",
		},
	}

	data, err := GenerateConfig(models, "dummy", "/path/to/llama-server", hardware.HardwareInfo{})
	if err != nil {
		t.Fatal(err)
	}

	yamlStr := string(data)
	if !strings.Contains(yamlStr, "Custom description here") {
		t.Error("should use custom description")
	}
}

func TestGenerateConfig_EmptyApiKey(t *testing.T) {
	models := []state.ModelEntry{
		{Slug: "m", Name: "M", Quant: "Q4", File: "m.gguf"},
	}

	data, err := GenerateConfig(models, "", "/path/to/llama-server", hardware.HardwareInfo{})
	if err != nil {
		t.Fatal(err)
	}

	yamlStr := string(data)
	if strings.Contains(yamlStr, "apiKeys:") {
		t.Error("should not emit apiKeys when apiKey is empty")
	}
}

func TestGenerateConfig_ModelPath(t *testing.T) {
	models := []state.ModelEntry{
		{Slug: "test-slug", Name: "Test", Quant: "Q4", File: "test.gguf"},
	}

	data, err := GenerateConfig(models, "dummy", "/path/to/llama-server", hardware.HardwareInfo{})
	if err != nil {
		t.Fatal(err)
	}

	yamlStr := string(data)
	home, _ := os.UserHomeDir()
	expectedPath := filepath.Join(home, "models", "test-slug", "test.gguf")
	if !strings.Contains(yamlStr, expectedPath) {
		t.Errorf("model path should be %q, got:\n%s", expectedPath, yamlStr)
	}
}

func TestGenerateConfig_MatchesUserConfigStructure(t *testing.T) {
	// This test validates that the generated YAML matches the structure of
	// the user's existing ~/.local/ai/config/llama-swap.yaml.
	models := []state.ModelEntry{
		{
			Slug:        "gemma4-chat",
			Name:        "Gemma 4 26B (Chat)",
			Description: "Multimodal chat - images, documents, general use - Q6_K",
			Quant:       "Q6_K",
			File:        "google_gemma-4-26B-A4B-it-Q6_K.gguf",
			Mmproj:      "mmproj-gemma-4-26B-A4B-it-Q8_0.gguf",
		},
		{
			Slug:        "qwen7b-fast",
			Name:        "Qwen2.5-Coder 7B Fast",
			Description: "7B Q4_K_M - inline suggestions and quick tasks",
			Quant:       "Q4_K_M",
			File:        "qwen2.5-coder-7b-instruct-q4_k_m.gguf",
			CtxSize:     16384,
		},
		{
			Slug:          "qwen35-medium",
			Name:          "Qwen3.6 27B Dense",
			Description:   "27B dense Q4_K_M - normal and heavy coding tasks",
			Quant:         "Q4_K_M",
			File:          "Qwen3.6-27B-Q4_K_M.gguf",
			CtxSize:       65536,
			Temperature:   0.7,
			TopK:          20,
			TopP:          0.8,
			RepeatPenalty: 1.05,
			Aliases: []string{
				"claude-sonnet-4-5",
				"claude-opus-4-5",
			},
		},
	}

	data, err := GenerateConfig(models, "dummy", "/Users/eduard/dev/llama.cpp/build/bin/llama-server", hardware.HardwareInfo{})
	if err != nil {
		t.Fatal(err)
	}

	yamlStr := string(data)
	t.Logf("generated config:\n%s", yamlStr)

	// Structural checks against user's existing file.
	checks := []struct {
		label    string
		mustHave string
	}{
		{"apiKeys top-level", "apiKeys:"},
		{"api key value", "- dummy"},
		{"models top-level", "models:"},
		{"gemma4-chat entry", "gemma4-chat:"},
		{"gemma name", "Gemma 4 26B (Chat)"},
		{"gemma mmproj in cmd", "--mmproj"},
		{"qwen7b ctx-size 16384", "--ctx-size 16384"},
		{"qwen35 sampling params", "--temp 0.70"},
		{"qwen35 top-k", "--top-k 20"},
		{"qwen35 top-p", "--top-p 0.80"},
		{"qwen35 repeat-penalty", "--repeat-penalty 1.05"},
		{"qwen35 aliases", "aliases:"},
		{"alias claude-sonnet", "- claude-sonnet-4-5"},
		{"literal block scalar", "cmd: |"},
		{"jinja flag", "--jinja"},
		{"ngl flag", "-ngl 999"},
	}

	for _, c := range checks {
		if !strings.Contains(yamlStr, c.mustHave) {
			t.Errorf("[%s] missing %q in generated YAML", c.label, c.mustHave)
		}
	}
}

// --- B6: WriteConfig / ForceWrite tests ---

// validTestYAML returns a minimal valid llama-swap YAML for write tests.
func validTestYAML() []byte {
	return []byte("models:\n  test:\n    name: Test\n    description: desc\n    cmd: |\n      /bin/echo hello\n")
}

func TestWriteConfig_FirstRun(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config", "llama-swap.yaml")
	content := validTestYAML()

	changed, diff, err := WriteConfig(path, content)
	if err != nil {
		t.Fatalf("WriteConfig failed: %v", err)
	}
	if !changed {
		t.Error("first run should report changed=true")
	}
	if diff != "" {
		t.Errorf("first run should have empty diff, got: %s", diff)
	}

	// Verify file was written.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not found after WriteConfig: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Errorf("content mismatch: wrote %d bytes, read %d bytes", len(content), len(data))
	}
}

func TestWriteConfig_NoChange(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "llama-swap.yaml")
	content := validTestYAML()

	// First write.
	changed1, _, err := WriteConfig(path, content)
	if err != nil {
		t.Fatal(err)
	}
	if !changed1 {
		t.Error("first write should be changed=true")
	}

	// Second write with identical content.
	changed2, diff, err := WriteConfig(path, content)
	if err != nil {
		t.Fatal(err)
	}
	if changed2 {
		t.Error("identical content should report changed=false")
	}
	if diff != "" {
		t.Errorf("identical content should have empty diff, got: %s", diff)
	}
}

func TestWriteConfig_Differs(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "llama-swap.yaml")

	// Write initial content.
	oldContent := []byte("models:\n  old-model:\n    name: Old\n    description: old\n    cmd: |\n      /bin/echo old\n")
	changed, _, err := WriteConfig(path, oldContent)
	if err != nil || !changed {
		t.Fatalf("initial write failed: changed=%v, err=%v", changed, err)
	}

	// Try to write different content.
	newContent := []byte("models:\n  new-model:\n    name: New\n    description: new\n    cmd: |\n      /bin/echo new\n")
	changed2, diff, err := WriteConfig(path, newContent)
	if err != nil {
		t.Fatalf("WriteConfig failed: %v", err)
	}
	if changed2 {
		t.Error("different content should report changed=false (not written yet)")
	}
	if diff == "" {
		t.Fatal("different content should return a non-empty diff")
	}

	// Verify the file was NOT overwritten.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, oldContent) {
		t.Error("file should not have been overwritten yet")
	}

	// Verify diff contains expected markers.
	if !strings.Contains(diff, "--- ") {
		t.Error("diff should contain '---' header")
	}
	if !strings.Contains(diff, "++") {
		t.Error("diff should contain '+++' header")
	}
	if !strings.Contains(diff, "old-model:") {
		t.Error("diff should show old model")
	}
	if !strings.Contains(diff, "new-model:") {
		t.Error("diff should show new model")
	}

	t.Logf("diff:\n%s", diff)
}

func TestWriteConfig_ThenForceWrite(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "llama-swap.yaml")

	// Write initial.
	oldContent := []byte("models:\n  old:\n    name: Old\n    description: old\n    cmd: |\n      /bin/echo old\n")
	_, _, err := WriteConfig(path, oldContent)
	if err != nil {
		t.Fatal(err)
	}

	// Try new content — should not overwrite.
	newContent := []byte("models:\n  new:\n    name: New\n    description: new\n    cmd: |\n      /bin/echo new\n")
	changed, diff, err := WriteConfig(path, newContent)
	if err != nil || changed || diff == "" {
		t.Fatalf("expected no-change with diff, got changed=%v, diff empty=%v", changed, diff == "")
	}

	// Force write.
	if err := ForceWrite(path, newContent); err != nil {
		t.Fatalf("ForceWrite failed: %v", err)
	}

	// Verify file now has new content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, newContent) {
		t.Error("ForceWrite should have overwritten with new content")
	}

	// Now WriteConfig with same content should report no change.
	changed3, _, err := WriteConfig(path, newContent)
	if err != nil || changed3 {
		t.Errorf("after ForceWrite, WriteConfig should report no change: changed=%v", changed3)
	}
}

func TestForceWrite_CreatesParentDirs(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "deep", "nested", "config.yaml")
	content := validTestYAML()

	if err := ForceWrite(path, content); err != nil {
		t.Fatalf("ForceWrite failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, content) {
		t.Error("content mismatch after ForceWrite")
	}
}

func TestComputeDiff(t *testing.T) {
	old := []byte("line1\nline2\nline3\n")
	new := []byte("line1\nmodified\nline3\nline4\n")

	diff := computeDiff("test.yaml", old, new)

	t.Logf("diff:\n%s", diff)

	if !strings.Contains(diff, "-line2\n") {
		t.Error("diff should show removed line2")
	}
	if !strings.Contains(diff, "+modified\n") {
		t.Error("diff should show added modified")
	}
	if !strings.Contains(diff, "+line4\n") {
		t.Error("diff should show added line4")
	}
	// Unchanged lines should be present.
	if !strings.Contains(diff, " line1\n") {
		t.Error("diff should show unchanged line1")
	}
	if !strings.Contains(diff, " line3\n") {
		t.Error("diff should show unchanged line3")
	}
}

func TestComputeDiff_Identical(t *testing.T) {
	content := []byte("same\nlines\n")
	diff := computeDiff("test.yaml", content, content)

	// Identical content should produce only header + context lines.
	if strings.Contains(diff, "-same") || strings.Contains(diff, "+same") {
		t.Error("identical content should have no +/- lines")
	}
}

func TestSplitLines(t *testing.T) {
	lines := splitLines([]byte("a\nb\nc\n"))
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %v", len(lines), lines)
	}

	lines2 := splitLines([]byte("single"))
	if len(lines2) != 1 || lines2[0] != "single" {
		t.Errorf("expected [\"single\"], got %v", lines2)
	}

	lines3 := splitLines([]byte{})
	if len(lines3) != 0 {
		t.Errorf("expected empty, got %v", lines3)
	}
}

// --- B7: ResolveLlamaServerPath tests ---

func TestResolveLlamaServerPath_FindsOnPath(t *testing.T) {
	path, err := ResolveLlamaServerPath()
	if err != nil {
		t.Skipf("llama-server not available: %v", err)
	}
	if path == "" {
		t.Fatal("resolved path should not be empty")
	}
	t.Logf("resolved llama-server: %s", path)
}

// --- B8: GenerateConfig empty-path handling ---

func TestGenerateConfig_EmptyPathResolves(t *testing.T) {
	path, err := ResolveLlamaServerPath()
	if err != nil {
		t.Skipf("llama-server not available for test: %v", err)
	}

	models := []state.ModelEntry{
		{Slug: "test", Name: "Test", Quant: "Q4", File: "test.gguf"},
	}

	data, err := GenerateConfig(models, "dummy", "", hardware.HardwareInfo{})
	if err != nil {
		t.Fatalf("GenerateConfig with empty path failed: %v", err)
	}

	yamlStr := string(data)
	if !strings.Contains(yamlStr, path) {
		t.Errorf("generated YAML should contain resolved path %q, got:\n%s", path, yamlStr)
	}
	if !strings.Contains(yamlStr, "cmd: |") {
		t.Error("cmd should use literal block scalar")
	}
}

func TestGenerateConfig_EmptyPath_NoBinary(t *testing.T) {
	saved := knownLlamaServerPaths
	knownLlamaServerPaths = []string{"/nonexistent/llama-server"}
	defer func() { knownLlamaServerPaths = saved }()

	t.Setenv("PATH", "")

	models := []state.ModelEntry{
		{Slug: "test", Name: "Test", Quant: "Q4", File: "test.gguf"},
	}

	_, err := GenerateConfig(models, "dummy", "", hardware.HardwareInfo{})
	if err == nil {
		t.Fatal("GenerateConfig with empty path and no binary should return an error")
	}
	if !strings.Contains(err.Error(), "llama-server") {
		t.Errorf("error should mention llama-server, got: %v", err)
	}
}

// --- B9: ValidateConfigYAML tests ---

func TestValidateConfigYAML_Valid(t *testing.T) {
	models := []state.ModelEntry{
		{Slug: "test", Name: "Test", Quant: "Q4", File: "test.gguf"},
	}
	data, err := GenerateConfig(models, "dummy", "/path/to/llama-server", hardware.HardwareInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfigYAML(data); err != nil {
		t.Errorf("valid config should pass validation: %v", err)
	}
}

func TestValidateConfigYAML_Tabs(t *testing.T) {
	badYAML := []byte("models:\n\ttest:\n\t\tname: Test\n\t\tcmd: |\n\t\t\t/bin/echo\n")
	err := ValidateConfigYAML(badYAML)
	if err == nil {
		t.Error("config with tab indentation should fail validation")
	}
	if !strings.Contains(err.Error(), "tab") {
		t.Errorf("error should mention tabs, got: %v", err)
	}
}

func TestValidateConfigYAML_SpacesOnly(t *testing.T) {
	models := []state.ModelEntry{
		{Slug: "test", Name: "Test", Quant: "Q4", File: "test.gguf"},
	}
	data, err := GenerateConfig(models, "dummy", "/opt/homebrew/bin/llama-server", hardware.HardwareInfo{})
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(data, []byte{'\t'}) {
		t.Error("generated YAML contains tab characters")
	}

	if err := yaml.Unmarshal(data, new(interface{})); err != nil {
		t.Errorf("generated YAML is not parseable: %v", err)
	}
}

func TestValidateConfigYAML_InvalidYAML(t *testing.T) {
	badYAML := []byte("models:\n  test:\n\tcmd: |\n    /bin/echo\n")
	err := ValidateConfigYAML(badYAML)
	if err == nil {
		t.Error("invalid YAML should fail validation")
	}
	if !strings.Contains(err.Error(), "tab") {
		t.Errorf("error should mention tabs for tab-containing content: %v", err)
	}
}

func TestValidateConfigYAML_EmptyCmd(t *testing.T) {
	badYAML := []byte("models:\n  test:\n    name: Test\n    cmd: ''\n")
	err := ValidateConfigYAML(badYAML)
	if err == nil {
		t.Error("empty cmd should fail validation")
	}
	if !strings.Contains(err.Error(), "empty cmd") {
		t.Errorf("error should mention empty cmd, got: %v", err)
	}
}

func TestValidateConfigYAML_FlagFirstLine(t *testing.T) {
	badYAML := []byte("models:\n  test:\n    name: Test\n    cmd: |\n      --host 127.0.0.1\n")
	err := ValidateConfigYAML(badYAML)
	if err == nil {
		t.Error("cmd starting with flag should fail validation")
	}
	if !strings.Contains(err.Error(), "flag") {
		t.Errorf("error should mention flag, got: %v", err)
	}
}

func TestGenerateConfig_IndentationConsistency(t *testing.T) {
	models := []state.ModelEntry{
		{Slug: "a", Name: "A", Quant: "Q4", File: "a.gguf"},
		{Slug: "b", Name: "B", Quant: "Q4", File: "b.gguf"},
	}
	data, err := GenerateConfig(models, "dummy", "/opt/homebrew/bin/llama-server", hardware.HardwareInfo{})
	if err != nil {
		t.Fatal(err)
	}

	yamlStr := string(data)
	lines := strings.Split(yamlStr, "\n")
	for i, line := range lines {
		if strings.Contains(line, "\t") {
			t.Errorf("line %d contains tab: %q", i+1, line)
		}
	}

	var parsed interface{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Errorf("final YAML is not parseable: %v", err)
	}

	reparsed := yaml.Unmarshal(data, new(interface{}))
	if reparsed != nil {
		t.Errorf("round-trip YAML parse failed: %v", reparsed)
	}
}

// --- B10: WriteConfig + ValidateConfigYAML integration ---

func TestWriteConfig_RejectsTabbedContent(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "llama-swap.yaml")
	content := []byte("models:\n\ttest:\n\t\tname: Test\n")

	_, _, err := WriteConfig(path, content)
	if err == nil {
		t.Fatal("WriteConfig should reject tab-containing content")
	}
}

func TestForceWrite_RejectsInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config", "llama-swap.yaml")
	content := []byte("models:\n  test:\n    cmd: ''\n")

	err := ForceWrite(path, content)
	if err == nil {
		t.Fatal("ForceWrite should reject content with empty cmd block")
	}
	if !strings.Contains(err.Error(), "refusing to write invalid config") {
		t.Errorf("error should mention refusing to write, got: %v", err)
	}
}

func TestWriteConfig_ForceWrite_ValidatesAfterMkdir(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "deep", "nested", "config.yaml")
	content := []byte("models:\n  test:\n    name: Test\n    cmd: ''\n")

	if err := ForceWrite(path, content); err == nil {
		t.Fatal("ForceWrite should reject content with empty cmd")
	}

	_, err := os.Stat(filepath.Dir(path))
	if !os.IsNotExist(err) {
		t.Error("parent directories should not be created for invalid content")
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"/a/b.gguf", "'/a/b.gguf'"},
		{"/a/my model.gguf", "'/a/my model.gguf'"},
		{"/a/model (1).gguf", "'/a/model (1).gguf'"},
		{`/a/it's.gguf`, "'/a/it'\\''s.gguf'"},
		{"", "''"},
	}
	for _, tt := range tests {
		if got := shellQuote(tt.in); got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGenerateConfig_PathWithSpacesAndParens(t *testing.T) {
	models := []state.ModelEntry{
		{Slug: "test-slug", Name: "Test", Quant: "Q4", File: "my model (1).gguf"},
	}

	data, err := GenerateConfig(models, "dummy", "/path/to/llama-server", hardware.HardwareInfo{})
	if err != nil {
		t.Fatal(err)
	}

	yamlStr := string(data)
	home, _ := os.UserHomeDir()
	want := fmt.Sprintf("--model '%s'", filepath.Join(home, "models", "test-slug", "my model (1).gguf"))
	if !strings.Contains(yamlStr, want) {
		t.Errorf("model path should be single-quoted as %q, got:\n%s", want, yamlStr)
	}
	if err := ValidateConfigYAML(data); err != nil {
		t.Errorf("generated config should validate: %v", err)
	}
}

func TestGenerateConfig_PathWithSingleQuote(t *testing.T) {
	models := []state.ModelEntry{
		{Slug: "test-slug", Name: "Test", Quant: "Q4", File: "it's.gguf"},
	}

	data, err := GenerateConfig(models, "dummy", "/path/to/llama-server", hardware.HardwareInfo{})
	if err != nil {
		t.Fatal(err)
	}

	yamlStr := string(data)
	home, _ := os.UserHomeDir()
	// A literal single quote must be escaped as '\'' inside the surrounding
	// single quotes so shlex re-joins it into the original character.
	want := fmt.Sprintf("--model '%s'", strings.ReplaceAll(filepath.Join(home, "models", "test-slug", "it's.gguf"), "'", `'\''`))
	if !strings.Contains(yamlStr, want) {
		t.Errorf("model path should escape the quote as %q, got:\n%s", want, yamlStr)
	}
	if err := ValidateConfigYAML(data); err != nil {
		t.Errorf("generated config should validate: %v", err)
	}
}

// --- helpers ---

func getRuntimeArch() string {
	out, err := exec.Command("go", "env", "GOARCH").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
