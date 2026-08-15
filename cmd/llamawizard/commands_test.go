package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The regression: `models add --link https://huggingface.co/unsloth/Qwen3.8-27B-GGUF`
// used to treat the last path segment as a filename and download the repo's
// HTML page as a model. A repo-page URL must be classified as a repo, never as
// a direct file, so it goes through the file-listing flow instead.
func TestClassifyLink_RepoPageIsNotADirectFile(t *testing.T) {
	kind, repo, filename := classifyLink("https://huggingface.co/unsloth/Qwen3.8-27B-GGUF")

	if kind != linkHFRepo {
		t.Fatalf("repo page classified as %q, want %q (must not be downloaded as a file)", kind, linkHFRepo)
	}
	if repo != "unsloth/Qwen3.8-27B-GGUF" {
		t.Errorf("repo = %q, want %q", repo, "unsloth/Qwen3.8-27B-GGUF")
	}
	if filename != "" {
		t.Errorf("filename = %q, want empty (a repo page has no file)", filename)
	}
}

func TestClassifyLink_ResolveLink(t *testing.T) {
	kind, repo, filename := classifyLink("https://huggingface.co/unsloth/Qwen3.8-27B-GGUF/resolve/main/Qwen3.8-27B-Q4_K_M.gguf")

	if kind != linkHFResolve {
		t.Fatalf("resolve link classified as %q, want %q", kind, linkHFResolve)
	}
	if repo != "unsloth/Qwen3.8-27B-GGUF" {
		t.Errorf("repo = %q, want %q", repo, "unsloth/Qwen3.8-27B-GGUF")
	}
	if filename != "Qwen3.8-27B-Q4_K_M.gguf" {
		t.Errorf("filename = %q, want %q", filename, "Qwen3.8-27B-Q4_K_M.gguf")
	}
}

func TestClassifyLink_BlobPageIsRepo(t *testing.T) {
	kind, repo, _ := classifyLink("https://huggingface.co/unsloth/Qwen3.8-27B-GGUF/blob/main/README.md")

	if kind != linkHFRepo {
		t.Fatalf("blob page classified as %q, want %q", kind, linkHFRepo)
	}
	if repo != "unsloth/Qwen3.8-27B-GGUF" {
		t.Errorf("repo = %q, want %q", repo, "unsloth/Qwen3.8-27B-GGUF")
	}
}

func TestClassifyLink_DirectGGUF(t *testing.T) {
	kind, repo, filename := classifyLink("https://example.com/models/foo-q4_k_m.gguf")

	if kind != linkDirect {
		t.Fatalf("direct gguf classified as %q, want %q", kind, linkDirect)
	}
	if repo != "" {
		t.Errorf("repo = %q, want empty", repo)
	}
	if filename != "foo-q4_k_m.gguf" {
		t.Errorf("filename = %q, want %q", filename, "foo-q4_k_m.gguf")
	}
}

func TestClassifyLink_Unknown(t *testing.T) {
	kind, _, _ := classifyLink("https://example.com/some/page")

	if kind != linkUnknown {
		t.Fatalf("non-gguf non-HF URL classified as %q, want %q", kind, linkUnknown)
	}
}

func TestExtractName(t *testing.T) {
	if got := extractName([]string{"--name", "my-model"}); got != "my-model" {
		t.Errorf("--name not honored: got %q", got)
	}
	if got := extractName([]string{"Qwen3.8-27B"}); got != "Qwen3.8-27B" {
		t.Errorf("positional name not honored: got %q", got)
	}
	if got := extractName([]string{}); got != "" {
		t.Errorf("expected empty name, got %q", got)
	}
}

func TestSlugFromFilename(t *testing.T) {
	cases := map[string]string{
		"Qwen3.8-27B-Q4_K_M.gguf": "qwen3.8-27b-q4-k-m",
		"Model.gguf":              "model",
		"Foo_Bar-Q8_0.gguf":       "foo-bar-q8-0",
	}
	for in, want := range cases {
		if got := slugFromFilename(in); got != want {
			t.Errorf("slugFromFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlugFromName(t *testing.T) {
	if got := slugFromName("Qwen3.8-27B"); got != "qwen3.8-27b" {
		t.Errorf("slugFromName = %q, want %q", got, "qwen3.8-27b")
	}
	if got := slugFromName("  My Model_Name  "); got != "my-model-name" {
		t.Errorf("slugFromName = %q, want %q", got, "my-model-name")
	}
}

func TestDeriveSlugIncludesQuant(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		want     string
	}{
		{"Qwen3.8-27B", "Qwen3.8-27B-UD-Q5_K_XL.gguf", "qwen3.8-27b-q5-k-xl"},
		{"Qwen3.8-27B", "Qwen3.8-27B-UD-Q4_K_XL.gguf", "qwen3.8-27b-q4-k-xl"},
		{"Qwen3.8-27B", "Qwen3.8-27B-IQ4_NL.gguf", "qwen3.8-27b-iq4-nl"},
		// Without a name, the filename-derived slug already carries the quant.
		{"", "Qwen3.8-27B-UD-Q5_K_XL.gguf", "qwen3.8-27b-ud-q5-k-xl"},
	}
	for _, c := range cases {
		if got := deriveSlug(c.name, c.filename); got != c.want {
			t.Errorf("deriveSlug(%q, %q) = %q, want %q", c.name, c.filename, got, c.want)
		}
	}
}

func TestNormalizeSlug(t *testing.T) {
	cases := map[string]string{
		"qwen3.6-27b":         "qwen3.6-27b-q4-k-m",
		"qwen3.8-27b-q5-k-xl": "qwen3.8-27b-q5-k-xl", // already has quant
	}
	for slug, want := range cases {
		quant := "Q4_K_M"
		if strings.Contains(slug, "q5-k-xl") {
			quant = "Q5_K_XL"
		}
		if got := normalizeSlug(slug, quant); got != want {
			t.Errorf("normalizeSlug(%q, %q) = %q, want %q", slug, quant, got, want)
		}
	}

	if got := normalizeSlug("foo", "custom"); got != "foo" {
		t.Errorf("normalizeSlug(foo, custom) = %q, want foo", got)
	}
	if got := normalizeSlug("foo", ""); got != "foo" {
		t.Errorf("normalizeSlug(foo, empty) = %q, want foo", got)
	}
}

func TestValidateGGUF_RejectsHTML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model.gguf")
	if err := os.WriteFile(path, []byte("<html><body>not a model</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := validateGGUF(path); err == nil {
		t.Fatal("expected HTML content to be rejected as non-GGUF, got nil error")
	}
}

func TestValidateGGUF_AcceptsGGUF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model.gguf")
	content := append([]byte("GGUF"), make([]byte, 64)...)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := validateGGUF(path); err != nil {
		t.Fatalf("expected valid GGUF magic to pass, got: %v", err)
	}
}
