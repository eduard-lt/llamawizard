package warlock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassifyServiceState(t *testing.T) {
	// Real captured failure: launchctl print exits 113 when the service is
	// not loaded; launchd.Status() wraps it like this.
	notLoadedErr := errors.New(`print: exit status 113
Bad request.
Could not find service "com.local.llama-swap" in domain for user gui: 501`)

	// Real captured output while running (first lines).
	runningDump := `gui/501/com.local.llama-swap = {
	active count = 1
	path = /Users/eduard/Library/LaunchAgents/com.local.llama-swap.plist
	type = LaunchAgent
	state = running
}`

	tests := []struct {
		name string
		out  string
		err  error
		want ServiceState
	}{
		{"not loaded (exit 113)", "", notLoadedErr, StateNotLoaded},
		{"running", runningDump, nil, StateRunning},
		{"not running", "state = not running", nil, StateNotRunning},
		{"exited", "state = exited", nil, StateNotRunning},
		{"waiting", "state = waiting", nil, StateNotRunning},
		{"unknown", "hello", nil, StateUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyServiceState(tt.out, tt.err); got != tt.want {
				t.Errorf("ClassifyServiceState() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServiceStateString(t *testing.T) {
	tests := []struct {
		s    ServiceState
		want string
	}{
		{StateUnknown, "unknown"},
		{StateRunning, "running"},
		{StateNotRunning, "not running"},
		{StateNotLoaded, "not loaded"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("ServiceState(%d).String() = %q, want %q", int(tt.s), got, tt.want)
		}
	}
}

func TestMonitorLifecycle(t *testing.T) {
	t0 := time.Date(2026, 8, 19, 21, 41, 0, 0, time.UTC)
	now := t0

	var startCalls []string
	m := NewMonitor("/tmp/plist.plist")
	m.Now = func() time.Time { return now }
	m.Start = func(plistPath string) error {
		startCalls = append(startCalls, plistPath)
		return nil
	}

	// Startup: running.
	m.Observe(StateRunning, "state = running", now)
	if !m.AutoRestart() {
		t.Error("auto-restart should default on")
	}
	evs := m.Events()
	if len(evs) != 1 || evs[0].Kind != "info" || evs[0].Message != "service running at startup" {
		t.Fatalf("startup events = %+v", evs)
	}

	// Death, 1 s later.
	now = t0.Add(1 * time.Second)
	m.Observe(StateNotLoaded, "print: exit status 113", now)
	if m.State() != StateNotLoaded {
		t.Errorf("State() = %v, want not loaded", m.State())
	}
	evs = m.Events()
	last := evs[len(evs)-1]
	if last.Kind != "death" || last.Message != "detected death: print: exit status 113" {
		t.Errorf("death event = %+v", last)
	}
	if !m.WantsRestart(now) {
		t.Error("WantsRestart should be true 1s after death")
	}

	// Restart in flight.
	m.BeginRestart(now)
	if m.WantsRestart(now) {
		t.Error("WantsRestart should be false while a restart is in flight")
	}
	m.Start(m.PlistPath)
	if len(startCalls) != 1 || startCalls[0] != "/tmp/plist.plist" {
		t.Errorf("startCalls = %v", startCalls)
	}
	m.FinishRestart(nil, now)
	evs = m.Events()
	last = evs[len(evs)-1]
	if last.Kind != "info" || last.Message != "restart command ok — waiting for service" {
		t.Errorf("finish event = %+v", last)
	}

	// Cooldown: false at +1 s, true at +6 s (5 s cooldown).
	if m.WantsRestart(now.Add(1 * time.Second)) {
		t.Error("WantsRestart should be false within the 5s cooldown")
	}
	now = now.Add(6 * time.Second)
	if !m.WantsRestart(now) {
		t.Error("WantsRestart should be true after the 5s cooldown")
	}

	// Failed restart: the event carries only the first line of the error.
	m.BeginRestart(now)
	m.Start(m.PlistPath)
	m.FinishRestart(errors.New("bootstrap: exit status 113\nsecond line"), now)
	evs = m.Events()
	last = evs[len(evs)-1]
	if last.Kind != "failed" || last.Message != "restart failed: bootstrap: exit status 113" {
		t.Errorf("failed event = %+v", last)
	}
	if strings.Contains(last.Message, "second line") {
		t.Error("failed event should contain only the first line of the error")
	}

	// Recovery 4.2 s later: death was at t0+1s, so the duration is 10.2 s.
	now = now.Add(4200 * time.Millisecond)
	m.Observe(StateRunning, "state = running", now)
	evs = m.Events()
	last = evs[len(evs)-1]
	if last.Kind != "recovered" || last.Message != "recovered after 10.2s" {
		t.Errorf("recovered event = %+v", last)
	}

	// Auto-restart off: a fresh death does not want a restart.
	m.SetAutoRestart(false)
	if m.AutoRestart() {
		t.Error("auto-restart should be off")
	}
	evs = m.Events()
	last = evs[len(evs)-1]
	if last.Kind != "info" || last.Message != "auto-restart off" {
		t.Errorf("toggle event = %+v", last)
	}
	m.Observe(StateNotRunning, "state = not running", now)
	if m.WantsRestart(now) {
		t.Error("WantsRestart should be false with auto-restart off")
	}
}

func TestMonitorStartupDown(t *testing.T) {
	now := time.Date(2026, 8, 19, 21, 41, 0, 0, time.UTC)
	m := NewMonitor("/tmp/plist.plist")
	m.Now = func() time.Time { return now }

	m.Observe(StateNotLoaded, "print: exit status 113", now)
	evs := m.Events()
	if len(evs) != 1 || evs[0].Kind != "info" ||
		evs[0].Message != "service down at startup: print: exit status 113" {
		t.Fatalf("events = %+v", evs)
	}

	// No death was observed, so coming back up emits no "recovered" event.
	m.Observe(StateRunning, "state = running", now.Add(3*time.Second))
	if evs := m.Events(); len(evs) != 1 {
		t.Fatalf("expected no extra events, got %+v", evs)
	}
}

func TestMonitorEventRingCap(t *testing.T) {
	now := time.Date(2026, 8, 19, 21, 41, 0, 0, time.UTC)
	m := NewMonitor("/tmp/plist.plist")
	m.Now = func() time.Time { return now }

	// 15 events: i even → "auto-restart on", i odd → "auto-restart off".
	for i := 0; i < 15; i++ {
		m.SetAutoRestart(i%2 == 0)
	}

	evs := m.Events()
	if len(evs) != 12 {
		t.Fatalf("len(Events()) = %d, want 12", len(evs))
	}
	// Oldest three evicted: first kept is i=3 (off), last is i=14 (on).
	if evs[0].Message != "auto-restart off" {
		t.Errorf("first kept event = %q, want auto-restart off", evs[0].Message)
	}
	if evs[11].Message != "auto-restart on" {
		t.Errorf("last kept event = %q, want auto-restart on", evs[11].Message)
	}
}

func TestFetchRecentLogs(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 19, 21, 47, 5, 0, time.UTC)

	stdoutPath := filepath.Join(dir, "llama-swap.log")
	errorPath := filepath.Join(dir, "llama-swap-error.log")

	stdoutContent := `[INFO] Request 127.0.0.1 "POST /v1/chat/completions HTTP/1.1" 200
[INFO] Request 127.0.0.1 "GET /v1/models HTTP/1.1" 200`
	// Both date-separator variants of the Go-log prefix are expected in the
	// error log (log package Ldate uses slashes; captured logs show dashes);
	// lines without a prefix get the observation time.
	errorContent := `2026/08/15 02:03:15 http: proxy error: dial tcp 127.0.0.1:5802: connect: connection refused
2026-08-15 02:03:16 plain line without timestamp
panic: runtime error: index out of range`

	if err := os.WriteFile(stdoutPath, []byte(stdoutContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(errorPath, []byte(errorContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Error log is newer: it wins.
	old := now.Add(-2 * time.Hour)
	newer := now.Add(-time.Minute)
	if err := os.Chtimes(errorPath, newer, newer); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stdoutPath, old, old); err != nil {
		t.Fatal(err)
	}

	source, lines := FetchRecentLogs(dir, 5, now)
	if source != "error" {
		t.Fatalf("source = %q, want error", source)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %+v", len(lines), lines)
	}
	if lines[0].Time != "02:03:15" || lines[0].Observed {
		t.Errorf("line 0 = %+v, want Time 02:03:15 Observed=false", lines[0])
	}
	if lines[0].Text != "http: proxy error: dial tcp 127.0.0.1:5802: connect: connection refused" {
		t.Errorf("line 0 text = %q (prefix should be stripped)", lines[0].Text)
	}
	if lines[1].Time != "02:03:16" || lines[1].Observed {
		t.Errorf("line 1 = %+v, want Time 02:03:16 Observed=false", lines[1])
	}
	if lines[1].Text != "plain line without timestamp" {
		t.Errorf("line 1 text = %q (prefix should be stripped)", lines[1].Text)
	}
	if lines[2].Time != "21:47:05" || !lines[2].Observed {
		t.Errorf("line 2 = %+v, want Time 21:47:05 Observed=true", lines[2])
	}
	if lines[2].Text != "panic: runtime error: index out of range" {
		t.Errorf("line 2 text = %q", lines[2].Text)
	}

	// Stdout log is newer now: it wins, plain lines get the observed time.
	if err := os.Chtimes(stdoutPath, newer, newer); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(errorPath, old, old); err != nil {
		t.Fatal(err)
	}

	source, lines = FetchRecentLogs(dir, 5, now)
	if source != "stdout" {
		t.Fatalf("source = %q, want stdout", source)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %+v", len(lines), lines)
	}
	for i, l := range lines {
		if l.Time != "21:47:05" || !l.Observed {
			t.Errorf("line %d = %+v, want Time 21:47:05 Observed=true", i, l)
		}
	}
	if lines[0].Text != `[INFO] Request 127.0.0.1 "POST /v1/chat/completions HTTP/1.1" 200` {
		t.Errorf("line 0 text = %q", lines[0].Text)
	}
}

func TestFetchRecentLogs_MissingDir(t *testing.T) {
	source, lines := FetchRecentLogs(filepath.Join(t.TempDir(), "nope"), 5, time.Now())
	if source != "" || lines != nil {
		t.Errorf("got (%q, %v), want (\"\", nil)", source, lines)
	}
}

func TestSnapshotResources(t *testing.T) {
	r := SnapshotResources()

	if r.RAMTotal == 0 {
		t.Error("RAMTotal should be > 0")
	}
	if r.RAMFree > r.RAMTotal {
		t.Errorf("RAMFree = %d, want <= RAMTotal %d", r.RAMFree, r.RAMTotal)
	}
	t.Logf("RAMTotal=%d RAMFree=%d", r.RAMTotal, r.RAMFree)
}
