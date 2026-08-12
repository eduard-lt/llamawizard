package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTryExistingBinary_FindsFakeBinary(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, ".local", "ai", "llama.cpp", "build", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	script := filepath.Join(binDir, "llama-server")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho llama-server version 1.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tmpDir)
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })

	path, ok := tryExistingBinary()
	if !ok {
		t.Fatal("expected to find fake binary")
	}
	if !strings.Contains(path, "llama-server") {
		t.Errorf("path = %q, want path containing llama-server", path)
	}
}

func TestTryExistingBinary_NonexistentHomePath(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, ".local", "ai", "llama.cpp", "build", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(binDir, "llama-server")
	if err := os.WriteFile(binary, []byte("not an executable"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tmpDir)
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })

	path, ok := tryExistingBinary()
	t.Logf("tryExistingBinary returned: path=%q ok=%v", path, ok)
	if ok && path == binary {
		t.Error("non-executable or broken binary at ~/.local/ai/ should not be accepted")
	}
}

func TestTryBrewInstall_NoBrew(t *testing.T) {
	if _, err := exec.LookPath("brew"); err == nil {
		t.Skip("brew is installed — tryBrewInstall may succeed meaningfully")
	}

	_, ok := tryBrewInstall()
	if ok {
		t.Error("tryBrewInstall should return false when brew is not available")
	}
}

func TestNproc_ReturnsPositive(t *testing.T) {
	n, err := nproc()
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Errorf("nproc = %d, want >= 1", n)
	}
}

func BenchmarkNproc(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = nproc()
	}
}
