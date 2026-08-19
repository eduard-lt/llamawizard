package binversion

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanVersionLine(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "llama.cpp multi-line",
			raw:  "version: 10360 (48d22e295)\nbuilt with AppleClang 21.0.0.21000101 for Darwin arm64\n",
			want: "10360 (48d22e295)",
		},
		{
			name: "llama-swap single line",
			raw:  "version: v245 (30470a4), built at 2026-07-31T04:56:21Z\n",
			want: "v245 (30470a4)",
		},
		{
			name: "no prefix fallback",
			raw:  "llama-server version 1.0\n",
			want: "llama-server version 1.0",
		},
		{
			name: "empty input",
			raw:  "",
			want: "",
		},
		{
			name: "capitalized prefix",
			raw:  "Version: 2.1\n",
			want: "2.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CleanVersionLine(tt.raw); got != tt.want {
				t.Errorf("CleanVersionLine(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestVersion_FakeBinary(t *testing.T) {
	tmpDir := t.TempDir()
	script := filepath.Join(tmpDir, "fake-llama")
	if err := os.WriteFile(script, []byte(
		"#!/bin/sh\necho 'version: 10360 (48d22e295)'\necho 'built with AppleClang 21'\n",
	), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Version(script)
	if err != nil {
		t.Fatalf("Version(%q) failed: %v", script, err)
	}
	if got != "10360 (48d22e295)" {
		t.Errorf("Version = %q, want %q", got, "10360 (48d22e295)")
	}
}

func TestVersion_NonZeroExit(t *testing.T) {
	tmpDir := t.TempDir()
	script := filepath.Join(tmpDir, "failing-llama")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Version(script); err == nil {
		t.Fatal("Version should fail when the binary exits non-zero")
	}
}
