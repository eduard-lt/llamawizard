// Package warlock implements the headless engine behind the `llamawizard
// warlock` guardian: service-state classification, a restart monitor with an
// event ring, log fetching, and resource snapshots. It has no TUI
// dependencies so it can be unit-tested headlessly.
package warlock

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/eduard-lt/llamawizard/internal/hardware"
	"github.com/eduard-lt/llamawizard/internal/launchd"
	"github.com/eduard-lt/llamawizard/internal/logtail"
)

// ServiceState is the classified state of the llama-swap LaunchAgent.
type ServiceState int

const (
	// StateUnknown means the state could not be determined from the output.
	StateUnknown ServiceState = iota
	// StateRunning means launchctl reports "state = running".
	StateRunning
	// StateNotRunning means the service is loaded but not running
	// ("state = not running", "state = exited", or "state = waiting").
	StateNotRunning
	// StateNotLoaded means launchctl print failed (e.g. exit 113).
	StateNotLoaded
)

// String returns the human-readable form of the state.
func (s ServiceState) String() string {
	switch s {
	case StateRunning:
		return "running"
	case StateNotRunning:
		return "not running"
	case StateNotLoaded:
		return "not loaded"
	default:
		return "unknown"
	}
}

// ClassifyServiceState maps the result of launchd.Status() to a ServiceState.
//
// A non-nil err (which covers the exit-113 "Bad request." / "Could not find
// service" failure) means the service is not loaded. Otherwise the output is
// scanned for the launchctl state line.
func ClassifyServiceState(out string, err error) ServiceState {
	if err != nil {
		return StateNotLoaded
	}
	if strings.Contains(out, "state = running") {
		return StateRunning
	}
	if strings.Contains(out, "state = not running") ||
		strings.Contains(out, "state = exited") ||
		strings.Contains(out, "state = waiting") {
		return StateNotRunning
	}
	return StateUnknown
}

// Event is one entry in the monitor's event ring.
type Event struct {
	Time    time.Time
	Kind    string // "death" | "restart" | "recovered" | "failed" | "info"
	Message string
}

// eventRingCap bounds how many events the monitor keeps (oldest evicted).
const eventRingCap = 12

// restartCooldown is the minimum interval between restart attempts.
const restartCooldown = 5 * time.Second

// Monitor watches the llama-swap service and records its state transitions.
//
// Start, Status, and Now are dependency-injected so tests can drive the
// monitor with a fake clock and scripted launchd behavior.
type Monitor struct {
	PlistPath string
	Start     func(plistPath string) error // default launchd.Start
	Status    func() (string, error)       // default launchd.Status
	Now       func() time.Time             // default time.Now

	autoRestart     bool
	state           ServiceState
	inFlight        bool
	lastAttempt     time.Time
	recovering      bool
	recoveringSince time.Time
	events          []Event
	observed        bool
}

// NewMonitor returns a Monitor for the LaunchAgent at plistPath.
func NewMonitor(plistPath string) *Monitor {
	return &Monitor{
		PlistPath:   plistPath,
		Start:       launchd.Start,
		Status:      launchd.Status,
		Now:         time.Now,
		autoRestart: true,
	}
}

// AutoRestart reports whether the monitor restarts the service when it dies.
func (m *Monitor) AutoRestart() bool {
	return m.autoRestart
}

// SetAutoRestart toggles automatic restart and records an info event.
func (m *Monitor) SetAutoRestart(on bool) {
	m.autoRestart = on
	msg := "auto-restart off"
	if on {
		msg = "auto-restart on"
	}
	m.pushEvent(Event{Time: m.Now(), Kind: "info", Message: msg})
}

// State returns the last observed service state.
func (m *Monitor) State() ServiceState {
	return m.state
}

// Events returns a copy of the event ring, oldest first.
func (m *Monitor) Events() []Event {
	out := make([]Event, len(m.events))
	copy(out, m.events)
	return out
}

// Observe records a new service-state observation.
//
// detail is caller-supplied context: the first line of the launchd.Status()
// error when it failed, or the matched "state = …" line otherwise.
func (m *Monitor) Observe(s ServiceState, detail string, now time.Time) {
	if !m.observed {
		m.observed = true
		if s == StateRunning {
			m.pushEvent(Event{Time: now, Kind: "info", Message: "service running at startup"})
		} else {
			m.pushEvent(Event{Time: now, Kind: "info", Message: "service down at startup: " + detail})
		}
	} else {
		switch {
		case m.state == StateRunning && s != StateRunning:
			m.recovering = true
			m.recoveringSince = now
			m.pushEvent(Event{Time: now, Kind: "death", Message: "detected death: " + detail})
		case m.state != StateRunning && s == StateRunning && m.recovering:
			m.pushEvent(Event{
				Time:    now,
				Kind:    "recovered",
				Message: "recovered after " + formatDuration(now.Sub(m.recoveringSince)),
			})
			m.recovering = false
		}
	}
	m.state = s
}

// WantsRestart reports whether a restart should be issued now: auto-restart
// is on, the service is not running, no restart is in flight, and at least
// restartCooldown has passed since the last attempt.
func (m *Monitor) WantsRestart(now time.Time) bool {
	return m.autoRestart &&
		m.state != StateRunning &&
		!m.inFlight &&
		now.Sub(m.lastAttempt) >= restartCooldown
}

// BeginRestart marks a restart as in flight and records a restart event.
func (m *Monitor) BeginRestart(now time.Time) {
	m.inFlight = true
	m.lastAttempt = now
	m.pushEvent(Event{Time: now, Kind: "restart", Message: "restart issued (kickstart/bootstrap)"})
}

// FinishRestart clears the in-flight flag and records the outcome.
func (m *Monitor) FinishRestart(err error, now time.Time) {
	m.inFlight = false
	if err != nil {
		m.pushEvent(Event{Time: now, Kind: "failed", Message: "restart failed: " + firstLine(err.Error())})
		return
	}
	m.pushEvent(Event{Time: now, Kind: "info", Message: "restart command ok — waiting for service"})
}

// pushEvent appends e to the ring, evicting the oldest entry past the cap.
func (m *Monitor) pushEvent(e Event) {
	m.events = append(m.events, e)
	if len(m.events) > eventRingCap {
		m.events = m.events[len(m.events)-eventRingCap:]
	}
}

// formatDuration renders d rounded to the nearest 0.1 s, e.g. "4.2s".
func formatDuration(d time.Duration) string {
	tenths := int64(math.Round(d.Seconds() * 10))
	return fmt.Sprintf("%d.%ds", tenths/10, tenths%10)
}

// firstLine returns the first line of s.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// LogLine is one line of a llama-swap log with a display timestamp.
type LogLine struct {
	Time     string // "HH:MM:SS"
	Observed bool   // true when the timestamp is warlock's observation time
	Text     string
}

// goLogPrefix matches the Go-log timestamp prefix used by the error log:
// "2026/08/15 02:03:15 …" (the log package's Ldate form) or the
// dash-separated date variant seen in captured logs.
var goLogPrefix = regexp.MustCompile(`^(\d{4})[-/](\d{2})[-/](\d{2}) (\d{2}):(\d{2}):(\d{2}) `)

// FetchRecentLogs returns the last n lines of the more recently modified of
// <logDir>/llama-swap.log (source "stdout") and
// <logDir>/llama-swap-error.log (source "error").
//
// Missing files are skipped; when neither exists it returns ("", nil). Lines
// carrying a Go-log timestamp keep it (Observed = false); all other lines get
// the observation time (Observed = true).
func FetchRecentLogs(logDir string, n int, now time.Time) (string, []LogLine) {
	type candidate struct {
		path   string
		source string
		mod    time.Time
	}
	cands := []candidate{
		{path: filepath.Join(logDir, "llama-swap.log"), source: "stdout"},
		{path: filepath.Join(logDir, "llama-swap-error.log"), source: "error"},
	}

	var best *candidate
	for i := range cands {
		info, err := os.Stat(cands[i].path)
		if err != nil {
			continue
		}
		cands[i].mod = info.ModTime()
		if best == nil || cands[i].mod.After(best.mod) {
			best = &cands[i]
		}
	}
	if best == nil {
		return "", nil
	}

	tail, err := logtail.Lines(best.path, n)
	if err != nil || tail == "" {
		return best.source, nil
	}

	lines := make([]LogLine, 0, len(strings.Split(tail, "\n")))
	for _, raw := range strings.Split(tail, "\n") {
		line := LogLine{Time: now.Format("15:04:05"), Observed: true, Text: raw}
		if m := goLogPrefix.FindStringSubmatch(raw); m != nil {
			line.Time = m[4] + ":" + m[5] + ":" + m[6]
			line.Observed = false
			line.Text = raw[len(m[0]):]
		}
		lines = append(lines, line)
	}
	return best.source, lines
}

// Resources is a snapshot of machine resource usage.
type Resources struct {
	RAMTotal uint64
	RAMFree  uint64
	Load1    float64
	Cores    int
	GPUModel string
	GPUUtil  string // e.g. "12%" or "N/A (sudo required)"
}

// gpuUnavailable is the GPUUtil value when utilization cannot be read.
const gpuUnavailable = "N/A (sudo required)"

// SnapshotResources samples RAM, CPU load, core count, and GPU utilization.
//
// It never returns an error: individual failures yield zero values or
// gpuUnavailable. gpuModel is passed in (the TUI fetches it once via
// system_profiler).
func SnapshotResources(gpuModel string) Resources {
	r := Resources{GPUModel: gpuModel, GPUUtil: gpuUnavailable}

	if total, err := hardware.TotalRAM(); err == nil {
		r.RAMTotal = total
	}
	if free, err := hardware.AvailableRAM(); err == nil {
		r.RAMFree = free
	}
	if load, err := loadAverage(); err == nil {
		r.Load1 = load
	}
	if cores, err := coreCount(); err == nil {
		r.Cores = cores
	}
	if util, err := gpuUtilization(); err == nil {
		r.GPUUtil = util
	}
	return r
}

// loadAverage reads the 1-minute load average from sysctl vm.loadavg.
//
// The output looks like "{ 3.43 3.97 3.62 }"; the first parseable float is
// returned.
func loadAverage() (float64, error) {
	out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	if err != nil {
		return 0, fmt.Errorf("sysctl vm.loadavg: %w", err)
	}
	for _, f := range strings.Fields(string(out)) {
		v, err := strconv.ParseFloat(f, 64)
		if err == nil {
			return v, nil
		}
	}
	return 0, fmt.Errorf("parsing load average from %q", string(out))
}

// coreCount reads the logical CPU count from sysctl hw.ncpu.
func coreCount() (int, error) {
	out, err := exec.Command("sysctl", "-n", "hw.ncpu").Output()
	if err != nil {
		return 0, fmt.Errorf("sysctl hw.ncpu: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("parsing hw.ncpu %q: %w", string(out), err)
	}
	return n, nil
}

// gpuUtilization samples GPU core utilization via powermetrics.
//
// Running as root needs no sudo; otherwise it requires cached sudo
// credentials (sudo -n). Any exec error, timeout, or parse failure yields an
// error so the caller can fall back to gpuUnavailable.
func gpuUtilization() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	var out []byte
	var err error
	if os.Geteuid() == 0 {
		out, err = exec.CommandContext(ctx, "powermetrics",
			"--samplers", "gpu_util", "-n", "1", "-i", "2000").Output()
	} else {
		out, err = exec.CommandContext(ctx, "sudo", "-n", "powermetrics",
			"--samplers", "gpu_util", "-n", "1", "-i", "2000").Output()
	}
	if err != nil {
		return "", fmt.Errorf("powermetrics: %w", err)
	}
	return parseGPUUtil(string(out))
}

// parseGPUUtil extracts the GPU core utilization percentage from
// powermetrics output. Real powermetrics output prefixes every line —
// including the header — with "date time powermetrics[pid:tid]", so the
// column position is measured within the header line itself: it finds the
// line containing "GPU Core Utilization", records the offset of that
// substring within the line, and takes the first whitespace-delimited token
// at (or after) that offset in the first subsequent non-blank line. Tokens
// that don't parse are skipped to tolerate minor format drift; the first
// token that does parse is the value and must be a percentage in [0, 100]
// (this also keeps the MHz frequency columns from being mistaken for it).
func parseGPUUtil(data string) (string, error) {
	idx := strings.Index(data, "GPU Core Utilization")
	if idx < 0 {
		return "", fmt.Errorf("no GPU Core Utilization line in powermetrics output")
	}

	// Start of the header line: the byte after the preceding newline.
	headerLineStart := 0
	if i := strings.LastIndexByte(data[:idx], '\n'); i >= 0 {
		headerLineStart = i + 1
	}
	col := idx - headerLineStart

	// Move past the header line to the start of the next line.
	nl := strings.IndexByte(data[idx:], '\n')
	if nl < 0 {
		return "", fmt.Errorf("no data line after GPU Core Utilization header")
	}
	pos := idx + nl + 1

	// Walk the subsequent lines; the first non-blank one holds the value. pos
	// tracks the absolute offset of the current line in data.
	for pos < len(data) {
		end := strings.IndexByte(data[pos:], '\n')
		var line string
		if end < 0 {
			line = data[pos:]
		} else {
			line = data[pos : pos+end]
		}
		if strings.TrimSpace(line) != "" {
			// Walk the line's whitespace-delimited tokens with their in-line
			// byte offsets. Tokens before the header column are ignored; a
			// token at (or after) it that doesn't parse is skipped to tolerate
			// minor format drift. The first token that does parse is the
			// value: it must be a percentage in [0, 100].
			var lastErr error
			for i := 0; i < len(line); {
				for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
					i++
				}
				if i >= len(line) {
					break
				}
				start := i
				for i < len(line) && line[i] != ' ' && line[i] != '\t' {
					i++
				}
				if start < col {
					continue
				}
				v, err := strconv.ParseFloat(line[start:i], 64)
				if err != nil {
					lastErr = err
					continue
				}
				if v < 0 || v > 100 {
					return "", fmt.Errorf("GPU utilization %v out of range [0, 100] on data line %q", v, line)
				}
				return fmt.Sprintf("%.0f%%", v), nil
			}
			if lastErr != nil {
				return "", fmt.Errorf("no parseable GPU utilization token on data line %q: %w", line, lastErr)
			}
			return "", fmt.Errorf("no parseable GPU utilization token on data line %q", line)
		}
		if end < 0 {
			break
		}
		pos += end + 1
	}
	return "", fmt.Errorf("no data line after GPU Core Utilization header")
}
