// Package logtail reads the tail of log files with bounded memory.
package logtail

import (
	"io"
	"os"
	"strings"
)

// maxWindow bounds how much of a file's end is read. 256 KiB holds far more
// than a few dozen lines of log text, so the tail is always complete in
// practice, while memory use stays constant no matter how large the log
// grows.
const maxWindow = 256 << 10

// Lines returns the last n lines of the file at path, or the entire file if
// it has fewer than n lines.
//
// Only a bounded window from the end of the file is read, so memory use is
// O(window), not O(file size). A final line without a trailing newline is
// included. If a single line is longer than the window, the returned tail
// contains the truncated end of that line.
func Lines(path string, n int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() == 0 {
		return "", nil
	}

	if info.Size() > maxWindow {
		if _, err := f.Seek(info.Size()-maxWindow, io.SeekStart); err != nil {
			return "", err
		}
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	for i, l := range lines {
		// Match bufio.ScanLines: strip the CR from CRLF line endings.
		lines[i] = strings.TrimSuffix(l, "\r")
	}

	if info.Size() > maxWindow && len(lines) > 1 {
		// The window starts mid-line (or right after a newline, giving an
		// empty first element): drop the partial first line. When the
		// window is a single line with no newline in it, it is the
		// truncated tail of one very long line, which is kept.
		lines = lines[1:]
	}

	if len(lines) == 1 && lines[0] == "" {
		return "", nil
	}

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}
