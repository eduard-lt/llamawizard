// Package logtail reads the tail of log files with bounded memory.
package logtail

import (
	"bytes"
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
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() == 0 {
		return "", nil
	}

	if info.Size() > maxWindow {
		// Read one extra byte before the window as well: whether it is a
		// newline tells us if the window starts on a line boundary.
		if _, err := f.Seek(info.Size()-maxWindow-1, io.SeekStart); err != nil {
			return "", err
		}
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}

	// len(data) can be 0 if the file was truncated (e.g. logrotate
	// copytruncate) between the Stat and the read; the stale size then
	// still claims the window path, so guard the indexing below.
	if info.Size() > maxWindow && len(data) > 0 {
		// data[0] is the byte before the window. If it is a newline, the
		// window starts on a line boundary and its first line is complete;
		// otherwise the first line is a fragment ending at the first
		// newline, or the whole window when it is one very long line.
		if data[0] == '\n' {
			data = data[1:]
		} else if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		} else {
			data = data[1:]
		}
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	for i, l := range lines {
		// Match bufio.ScanLines: strip the CR from CRLF line endings.
		lines[i] = strings.TrimSuffix(l, "\r")
	}

	if len(lines) == 1 && lines[0] == "" {
		return "", nil
	}

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}
