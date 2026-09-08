package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModelDirRejectsTraversalAndSymlinks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, slug := range []string{"", ".", "..", "../other", "a/b", "a\\b"} {
		if _, err := ModelDir(slug); err == nil {
			t.Errorf("accepted %q", slug)
		}
	}
	if err := os.MkdirAll(filepath.Join(home, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(home, filepath.Join(home, "models", "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := ModelDir("linked"); err == nil {
		t.Fatal("accepted symlink")
	}
	if _, err := ModelDir("qwen-4.0-q4-k-m"); err != nil {
		t.Fatal(err)
	}
}
