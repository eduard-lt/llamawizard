package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tmpPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "state.json")
}

func mustNow() *time.Time {
	now := time.Now().UTC()
	return &now
}

func TestLoadFileNotFound(t *testing.T) {
	s, err := Load(filepath.Join(os.TempDir(), "llamawizard-nonexistent-state.json"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil zero-value State")
	}
	if s.Port != 0 {
		t.Errorf("expected Port 0, got %d", s.Port)
	}
	if s.Models != nil {
		t.Errorf("expected nil Models, got %+v", s.Models)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := tmpPath(t)

	healthCheck := mustNow()
	original := &State{
		Port:            8080,
		APIKey:          "dummy",
		Chip:            "apple-silicon",
		LlamaCppPath:    "/opt/homebrew/bin/llama-server",
		LlamaSwapPath:   "/opt/homebrew/bin/llama-swap",
		LastHealthCheck: healthCheck,
		Models: []ModelEntry{
			{
				Slug:        "gemma4-26b",
				HFRepo:      "ggml-org/gemma-4-26B-A4B-it-GGUF",
				Quant:       "Q4_K_M",
				File:        "gemma-4-26B-A4B-it-Q4_K_M.gguf",
				Mmproj:      "mmproj-gemma-4-26B-A4B-it-bf16.gguf",
				SizeBytes:   18042093568,
				InstalledAt: "2026-08-10T12:00:00Z",
			},
			{
				Slug:        "qwen2.5-7b",
				HFRepo:      "bartowski/Qwen2.5-7B-Instruct-GGUF",
				Quant:       "Q8_0",
				File:        "Qwen2.5-7B-Instruct-Q8_0.gguf",
				SizeBytes:   8589934592,
				InstalledAt: "2026-08-10T12:05:00Z",
			},
		},
	}

	if err := original.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Port != original.Port {
		t.Errorf("Port: want %d, got %d", original.Port, loaded.Port)
	}
	if loaded.APIKey != original.APIKey {
		t.Errorf("APIKey: want %q, got %q", original.APIKey, loaded.APIKey)
	}
	if loaded.Chip != original.Chip {
		t.Errorf("Chip: want %q, got %q", original.Chip, loaded.Chip)
	}
	if loaded.LlamaCppPath != original.LlamaCppPath {
		t.Errorf("LlamaCppPath mismatch")
	}
	if loaded.LlamaSwapPath != original.LlamaSwapPath {
		t.Errorf("LlamaSwapPath mismatch")
	}
	if len(loaded.Models) != len(original.Models) {
		t.Fatalf("Models count: want %d, got %d", len(original.Models), len(loaded.Models))
	}
	for i, m := range original.Models {
		if loaded.Models[i].Slug != m.Slug {
			t.Errorf("Models[%d].Slug: want %q, got %q", i, m.Slug, loaded.Models[i].Slug)
		}
		if loaded.Models[i].HFRepo != m.HFRepo {
			t.Errorf("Models[%d].HFRepo mismatch", i)
		}
		if loaded.Models[i].SizeBytes != m.SizeBytes {
			t.Errorf("Models[%d].SizeBytes: want %d, got %d", i, m.SizeBytes, loaded.Models[i].SizeBytes)
		}
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "state.json")

	s := &State{Port: 3000}
	if err := s.Save(path); err != nil {
		t.Fatalf("Save to nested path failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Port != 3000 {
		t.Errorf("Port: want 3000, got %d", loaded.Port)
	}
}

func TestDeriveSlug(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Qwen-Qwen3.6-27B-Q4_K_M.gguf", "Qwen3.6-27B-Q4_K_M"},
		{"gemma-4-26B-A4B-it-Q4_K_M.gguf", "4-26B-A4B-it-Q4_K_M"},
		{"google_gemma-4-26B-A4B-it-Q6_K.gguf", "4-26B-A4B-it-Q6_K"},
		{"Qwen2.5-7B-Instruct-Q8_0.gguf", "7B-Instruct-Q8_0"},
		{"qwen2.5-coder-7b-instruct-q4_k_m.gguf", "coder-7b-instruct-q4_k_m"},
		{"model.gguf", "model"},
		{"no-extension", "no-extension"},                       // no .gguf → no stripping
		{"-has-prefix-Q4.gguf", "-has-prefix-Q4"},              // leading hyphen, idx==0 → no strip
		{"Qwen-Qwen3.6-27B-Q4_K_M.GGUF", "Qwen3.6-27B-Q4_K_M"}, // case-insensitive ext

	}

	for _, tc := range tests {
		got := DeriveSlug(tc.input)
		if got != tc.expected {
			t.Errorf("DeriveSlug(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestDeriveSlugFromModelID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ggml-org/gemma-4-26B-A4B-it-GGUF", "gemma-4-26b-a4b-it-gguf"},
		{"bartowski/Qwen2.5-7B-Instruct-GGUF", "qwen2.5-7b-instruct-gguf"},
		{"Qwen/Qwen3-8B", "qwen3-8b"},
		{"google_gemma-4-26B-A4B-it", "google-gemma-4-26b-a4b-it"},
		{"simple/model", "model"},
		{"no-slash", "no-slash"},
	}

	for _, tc := range tests {
		got := DeriveSlugFromModelID(tc.input)
		if got != tc.expected {
			t.Errorf("DeriveSlugFromModelID(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestDefaultPath(t *testing.T) {
	p := DefaultPath()
	if p == "" {
		t.Fatal("DefaultPath returned empty string")
	}
	if filepath.Base(p) != "state.json" {
		t.Errorf("expected state.json, got %s", filepath.Base(p))
	}
}
