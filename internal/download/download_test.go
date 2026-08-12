package download

import (
	"testing"
)

func TestResolveFilesMultimodal(t *testing.T) {
	files, err := ResolveFiles("ggml-org/gemma-4-26B-A4B-it-GGUF", "Q8_0")
	if err != nil {
		t.Fatalf("ResolveFiles failed: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files (main GGUF + mmproj), got %d", len(files))
	}

	main := files[0]
	if main.IsMmproj {
		t.Error("first file should be the main GGUF, not mmproj")
	}
	if !stringsHasSuffix(main.Filename, "Q8_0.gguf") {
		t.Errorf("main filename should end with Q8_0.gguf, got %s", main.Filename)
	}
	if main.Size == 0 {
		t.Error("main file size should not be 0")
	}

	mmproj := files[1]
	if !mmproj.IsMmproj {
		t.Error("second file should be the mmproj")
	}
	if !stringsHasPrefix(mmproj.Filename, "mmproj-") {
		t.Errorf("mmproj filename should start with mmproj-, got %s", mmproj.Filename)
	}

	t.Logf("Main:  %s (%d bytes)", main.Filename, main.Size)
	t.Logf("Mmproj: %s (%d bytes)", mmproj.Filename, mmproj.Size)
}

func TestResolveFilesMultimodalNoMmproj(t *testing.T) {
	// Q4_0 exists but has no mmproj companion in this repo.
	files, err := ResolveFiles("ggml-org/gemma-4-26B-A4B-it-GGUF", "Q4_0")
	if err != nil {
		t.Fatalf("ResolveFiles failed: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file (no mmproj for Q4_0), got %d", len(files))
	}
	if files[0].IsMmproj {
		t.Error("should be the main GGUF")
	}
	t.Logf("Main: %s (%d bytes)", files[0].Filename, files[0].Size)
}

func TestResolveFilesTextOnly(t *testing.T) {
	files, err := ResolveFiles("bartowski/Qwen2.5-7B-Instruct-GGUF", "Q4_K_M")
	if err != nil {
		t.Fatalf("ResolveFiles failed: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file (text-only model), got %d", len(files))
	}
	f := files[0]
	if f.IsMmproj {
		t.Error("text-only model should not have mmproj")
	}
	if !stringsHasSuffix(f.Filename, "Q4_K_M.gguf") {
		t.Errorf("expected Q4_K_M.gguf, got %s", f.Filename)
	}
	t.Logf("Main: %s (%d bytes)", f.Filename, f.Size)
}

func TestResolveFilesNotFound(t *testing.T) {
	_, err := ResolveFiles("ggml-org/gemma-4-26B-A4B-it-GGUF", "Q99_FAKE")
	if err == nil {
		t.Fatal("expected error for nonexistent quant, got nil")
	}
	t.Logf("Got expected error: %v", err)
}

// helpers to avoid importing strings in test (already imported in main pkg).
func stringsHasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
