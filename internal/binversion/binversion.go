// Package binversion determines the self-reported version of external
// binaries (llama.cpp, llama-swap) by running them with --version.
//
// Versions are probed live at call time and never stored: every call runs
// the binary, so the reported version always reflects what is on disk.
package binversion

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// versionTimeout bounds how long a single --version probe may run. It keeps
// the version command from hanging on a wedged binary.
const versionTimeout = 5 * time.Second

// versionPrefix matches a leading "version" label (case-insensitive, with an
// optional colon), e.g. "version: 10360 (abc)" or "Version 2.1".
var versionPrefix = regexp.MustCompile(`(?i)^version\s*:?\s+`)

// Version runs "path --version" with a timeout and returns the cleaned
// first line of the combined output.
//
// It returns an error when the binary cannot be started, exits non-zero,
// times out, or produces no usable output.
func Version(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("running %q --version: %w", path, err)
	}

	if strings.TrimSpace(string(out)) == "" {
		return "", fmt.Errorf("%s --version produced no output", path)
	}

	return CleanVersionLine(string(out)), nil
}

// CleanVersionLine reduces raw --version output to a single displayable
// version string: the first line, trimmed, with a leading "version" label
// (optional colon, case-insensitive) removed and everything from the first
// ", built at" marker onward cut. If cleaning leaves the line empty, the
// raw trimmed first line is returned instead.
func CleanVersionLine(raw string) string {
	first := strings.TrimSpace(firstLine(raw))

	cleaned := versionPrefix.ReplaceAllString(first, "")
	if i := strings.Index(cleaned, ", built at"); i >= 0 {
		cleaned = cleaned[:i]
	}
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return first
	}
	return cleaned
}

// firstLine returns the first line of s, without any line terminator.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
