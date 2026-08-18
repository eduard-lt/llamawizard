package logtail

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLog(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.log")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLines_FewerThanN(t *testing.T) {
	got, err := Lines(writeLog(t, "a\nb\nc\n"), 20)
	if err != nil {
		t.Fatal(err)
	}
	if got != "a\nb\nc" {
		t.Errorf("got %q, want %q", got, "a\nb\nc")
	}
}

func TestLines_NoTrailingNewline(t *testing.T) {
	got, err := Lines(writeLog(t, "a\nb\nc"), 20)
	if err != nil {
		t.Fatal(err)
	}
	if got != "a\nb\nc" {
		t.Errorf("got %q, want %q", got, "a\nb\nc")
	}
}

func TestLines_TakesLastN(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 25; i++ {
		fmt.Fprintf(&b, "line %02d\n", i)
	}
	got, err := Lines(writeLog(t, b.String()), 20)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 20 {
		t.Fatalf("got %d lines, want 20", len(lines))
	}
	if lines[0] != "line 05" || lines[19] != "line 24" {
		t.Errorf("unexpected tail: first %q, last %q", lines[0], lines[19])
	}
}

func TestLines_FileLargerThanWindow(t *testing.T) {
	// ~300 KB of 1 KB lines: bigger than the 256 KiB window, so the read
	// starts mid-file and the partial first line must be dropped.
	var b strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&b, "line %03d: %s\n", i, strings.Repeat("x", 1000))
	}
	got, err := Lines(writeLog(t, b.String()), 20)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 20 {
		t.Fatalf("got %d lines, want 20", len(lines))
	}
	if !strings.HasPrefix(lines[0], "line 280: ") {
		t.Errorf("first tail line should be a complete line, got %q...", lines[0][:20])
	}
	if !strings.HasPrefix(lines[19], "line 299: ") {
		t.Errorf("last tail line should be line 299, got %q...", lines[19][:20])
	}
}

func TestLines_FileLargerThanWindow_NoTrailingNewline(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&b, "line %03d: %s", i, strings.Repeat("x", 1000))
		if i < 299 {
			b.WriteByte('\n')
		}
	}
	got, err := Lines(writeLog(t, b.String()), 20)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 20 {
		t.Fatalf("got %d lines, want 20", len(lines))
	}
	if !strings.HasPrefix(lines[19], "line 299: ") {
		t.Errorf("final unterminated line must be included, got %q...", lines[19][:20])
	}
}

func TestLines_WindowStartsOnLineBoundary(t *testing.T) {
	// 22 lines of exactly 16 KiB each: the 256 KiB window starts exactly
	// on the boundary before line 6, so all 16 window lines are complete
	// and none may be dropped. The window holds fewer than 20+1 lines, so
	// a wrongly dropped line would show up in the tail.
	const lineLen = 16 << 10
	var b strings.Builder
	for i := 0; i < 22; i++ {
		fmt.Fprintf(&b, "line %02d: %s\n", i, strings.Repeat("x", lineLen-10))
	}
	got, err := Lines(writeLog(t, b.String()), 20)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 16 {
		t.Fatalf("got %d lines, want 16 (the window holds exactly 16 complete lines)", len(lines))
	}
	if !strings.HasPrefix(lines[0], "line 06: ") {
		t.Errorf("first tail line must be the complete boundary line, got %q...", lines[0][:20])
	}
	if !strings.HasPrefix(lines[15], "line 21: ") {
		t.Errorf("last tail line must be line 21, got %q...", lines[15][:20])
	}
}

func TestLines_SingleLineLongerThanWindow(t *testing.T) {
	// One 300 KB line with no newlines: the bounded read must not load the
	// whole line, and must return the truncated tail instead of nothing.
	p := writeLog(t, strings.Repeat("x", 300<<10))
	got, err := Lines(p, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != maxWindow {
		t.Fatalf("got %d bytes, want the %d-byte window", len(got), maxWindow)
	}
	if got != strings.Repeat("x", maxWindow) {
		t.Errorf("tail should be all x's, got %d bytes", len(got))
	}
}

func TestLines_EmptyFile(t *testing.T) {
	got, err := Lines(writeLog(t, ""), 20)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestLines_MissingFile(t *testing.T) {
	_, err := Lines(filepath.Join(t.TempDir(), "nope.log"), 20)
	if err == nil {
		t.Error("expected an error for a missing file")
	}
}
