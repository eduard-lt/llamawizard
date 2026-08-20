package warlock

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eduard-lt/llamawizard/internal/ripple"
	"github.com/eduard-lt/llamawizard/internal/state"
)

// update drives the model and asserts the result is a warlock Model.
func update(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

// runCmd executes cmd and returns the messages it produces, expanding
// batches (tea.Batch collapses to a plain cmd when only one survives).
func runCmd(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		out := make([]tea.Msg, 0, len(msg))
		for _, c := range msg {
			out = append(out, c())
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

// testModel builds a dashboard at 100x30 with a scripted monitor, a few
// events, and populated logs/resources.
func testModel(t *testing.T) Model {
	t.Helper()
	plist := t.TempDir() + "/com.local.llama-swap.plist"
	m := InitialModel(plist, &state.State{Port: 8080}, "v0.1.6")
	m.width = 100
	m.height = 30
	m.rip.SetSize(m.width, m.height)

	now := time.Date(2026, 8, 19, 21, 41, 0, 0, time.Local)
	m.mon.Now = func() time.Time { return now }
	m.mon.Start = func(plistPath string) error { return nil }
	// Fake Status so poll cmds returned by Update are safe to execute.
	m.mon.Status = func() (string, error) { return "state = running", nil }

	m.mon.Observe(StateRunning, "state = running", now)
	m.upSince = now
	now = now.Add(61 * time.Second)
	m.mon.Observe(StateNotLoaded, "print: exit status 113", now)
	m.downSince = now
	m.mon.BeginRestart(now)
	m.mon.FinishRestart(errors.New("bootstrap: exit status 113"), now.Add(2*time.Second))
	now = now.Add(4200 * time.Millisecond)
	m.mon.Observe(StateRunning, "state = running", now)
	m.upSince = now

	m.logs = []LogLine{
		{Time: "21:47:01", Observed: false, Text: `[INFO] Request 127.0.0.1 "POST /v1/chat/completions HTTP/1.1" 200`},
		{Time: "21:47:05", Observed: true, Text: "plain line without a timestamp"},
	}
	m.logSource = "stdout"
	m.res = Resources{
		RAMTotal: 36 << 30,
		RAMFree:  14 << 30, // 14.0 GiB free → 22.0 GiB used
	}
	return m
}

func TestUIInitialModel(t *testing.T) {
	m := InitialModel("/tmp/plist.plist", &state.State{Port: 8080}, "v0.1.6")
	if m.port != 8080 {
		t.Errorf("port = %d, want 8080", m.port)
	}
	if m.mon == nil || m.mon.PlistPath != "/tmp/plist.plist" {
		t.Errorf("mon = %+v, want monitor for /tmp/plist.plist", m.mon)
	}
	if !m.mon.AutoRestart() {
		t.Error("auto-restart should default on")
	}
	if m.version != "v0.1.6" {
		t.Errorf("version = %q", m.version)
	}

	// A nil state must not panic and yields port N/A.
	m2 := InitialModel("/tmp/plist.plist", nil, "dev")
	if m2.port != 0 {
		t.Errorf("port = %d, want 0 for nil state", m2.port)
	}
}

func TestUIView(t *testing.T) {
	m := testModel(t)
	v := m.View()

	for _, want := range []string{
		"llamawarlock v0.1.6",
		"SERVICE",
		"port 8080",
		"LAST 5 LOGS",
		"stdout",
		"EVENTS",
		"RESOURCES",
		"22.0 / 36.0 GiB",
		"up since 21:42:05",
		// Event messages are truncated to the panel width; assert stable prefixes.
		"detected death: print: exit",
		"recovered after 4.2s",
		"restart failed: bootstrap",
		// The log line is truncated to the panel width; assert the stable prefix.
		`[INFO] Request 127.0.0.1 "POST`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("View() missing %q\n---\n%s", want, v)
		}
	}

	// The rain pane carries the blue foreground, like the welcome screen.
	if !strings.Contains(v, "\x1b[34m") {
		t.Error("View() missing the blue rain foreground")
	}
}

func TestCompositeField(t *testing.T) {
	field := make([][]rune, 4)
	for y := 0; y < 4; y++ {
		field[y] = make([]rune, 12)
		for x := 0; x < 12; x++ {
			field[y][x] = '@'
		}
	}
	block := [][]cell{{
		{'A', wlOK}, {' ', ""}, {'B', wlOK}, {' ', ""}, {' ', ""}, {'C', wlErr},
	}}
	out := compositeField(field, block, 2, 1)
	rows := strings.Split(out, "\n")
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want 4", len(rows))
	}
	for i, row := range rows {
		if !strings.HasPrefix(row, wlRain) {
			t.Errorf("row %d does not start with the rain color: %q", i, row)
		}
		if !strings.HasSuffix(row, "\x1b[0m") {
			t.Errorf("row %d does not end with the reset code: %q", i, row)
		}
	}

	// Rows outside the block are pure rain.
	for _, i := range []int{0, 2, 3} {
		if rows[i] != wlRain+strings.Repeat("@", 12)+"\x1b[0m" {
			t.Errorf("row %d = %q, want pure rain", i, rows[i])
		}
	}

	// Row 1: A and B under wlOK, the two space cells render as '@', C under
	// wlErr.
	r1 := rows[1]
	if !strings.Contains(r1, wlOK+"A") || !strings.Contains(r1, wlOK+"B") {
		t.Errorf("row 1 missing A/B under the wlOK code: %q", r1)
	}
	if !strings.Contains(r1, wlErr+"C") {
		t.Errorf("row 1 missing C under the wlErr code: %q", r1)
	}
	if !strings.Contains(r1, "@@") {
		t.Errorf("row 1 space cells should render as rain '@': %q", r1)
	}
	// The visible (non-escape) content is the full 12-column row: the block's
	// space cells show the rain '@'.
	visible := stripANSI(r1)
	if visible != "@@A@B@@C@@@@" {
		t.Errorf("row 1 visible content = %q, want %q", visible, "@@A@B@@C@@@@")
	}
}

// stripANSI removes ANSI escape sequences from s.
func stripANSI(s string) string {
	var sb strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			j := i + 1
			for j < len(s) && s[j] != 'm' {
				j++
			}
			i = j + 1
			continue
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}

func TestUIViewSmallTerminal(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 60, 20
	if v := m.View(); !strings.Contains(v, "terminal too small") {
		t.Errorf("small terminal view = %q", v)
	}

	// Before the first window size message the view must not panic.
	m2 := InitialModel("/tmp/plist.plist", &state.State{Port: 8080}, "v0.1.6")
	if v := m2.View(); !strings.Contains(v, "terminal too small") {
		t.Errorf("zero-size view = %q", v)
	}
}

func TestUIKeys(t *testing.T) {
	m := testModel(t)

	// r toggles auto-restart.
	if !m.mon.AutoRestart() {
		t.Fatal("precondition: auto-restart on")
	}
	m, cmd := update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd != nil {
		t.Errorf("r returned cmd %v, want nil", cmd)
	}
	if m.mon.AutoRestart() {
		t.Error("r should turn auto-restart off")
	}
	m, _ = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if !m.mon.AutoRestart() {
		t.Error("second r should turn auto-restart back on")
	}

	// q quits.
	if _, cmd = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}); cmd == nil {
		t.Error("q should return the quit cmd")
	}
	// esc quits too.
	if _, cmd = update(m, tea.KeyMsg{Type: tea.KeyEsc}); cmd == nil {
		t.Error("esc should return the quit cmd")
	}
}

func TestUIRestartKey(t *testing.T) {
	m := testModel(t)
	var started string
	m.mon.Start = func(plistPath string) error {
		started = plistPath
		return nil
	}

	_, cmd := update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd == nil {
		t.Fatal("a should return the restart cmd")
	}
	if msg := cmd(); msg.(restartDoneMsg).err != nil {
		t.Errorf("restart cmd error: %v", msg)
	}
	if started != m.mon.PlistPath {
		t.Errorf("Start called with %q, want %q", started, m.mon.PlistPath)
	}

	// FinishRestart records the outcome.
	m, _ = update(m, restartDoneMsg{err: nil})
	evs := m.mon.Events()
	last := evs[len(evs)-1]
	if last.Kind != "info" || !strings.Contains(last.Message, "restart command ok") {
		t.Errorf("finish event = %+v", last)
	}
}

func TestUIStatusResult(t *testing.T) {
	m := testModel(t)
	// observeStatus uses the real clock; clear the scripted (future) cooldown
	// so the monitor wants a restart on the next death.
	m.mon.lastAttempt = time.Now().Add(-time.Minute)
	var started string
	m.mon.Start = func(plistPath string) error {
		started = plistPath
		return nil
	}

	// Death: the monitor wants a restart (auto-restart on, cooldown passed),
	// so handling the result must issue the restart cmd.
	m, cmd := update(m, statusResultMsg{
		err: errors.New("print: exit status 113\nBad request."),
	})
	if cmd == nil {
		t.Fatal("dead service with auto-restart on should return the restart cmd")
	}
	// The result is batched with the next status poll; executing it must run
	// the restart (and the fake status poll).
	var restarts, polls int
	for _, msg := range runCmd(t, cmd) {
		switch msg := msg.(type) {
		case restartDoneMsg:
			restarts++
			if msg.err != nil {
				t.Errorf("restart cmd error: %v", msg)
			}
		case statusResultMsg:
			polls++
			if msg.out != "state = running" {
				t.Errorf("status poll out = %q, err = %v", msg.out, msg.err)
			}
		}
	}
	if restarts != 1 || polls != 1 {
		t.Errorf("ran %d restart + %d status cmds, want 1 + 1", restarts, polls)
	}
	if started != m.mon.PlistPath {
		t.Errorf("Start called with %q, want %q", started, m.mon.PlistPath)
	}
	if m.mon.State() != StateNotLoaded {
		t.Errorf("State() = %v, want not loaded", m.mon.State())
	}
	if m.downSince.IsZero() {
		t.Error("downSince should be set on running→dead")
	}
	evs := m.mon.Events()
	last := evs[len(evs)-1]
	if last.Kind != "restart" {
		t.Errorf("last event = %+v, want restart", last)
	}

	// Recovery: upSince is set and no restart is issued (only the
	// rescheduled status poll runs).
	m, cmd = update(m, statusResultMsg{out: "state = running"})
	for _, msg := range runCmd(t, cmd) {
		if _, ok := msg.(restartDoneMsg); ok {
			t.Error("running service should not issue a restart")
		}
	}
	if m.mon.State() != StateRunning {
		t.Errorf("State() = %v, want running", m.mon.State())
	}
	if m.upSince.IsZero() {
		t.Error("upSince should be set on dead→running")
	}
}

func TestUIPollResults(t *testing.T) {
	m := testModel(t)

	m, cmd := update(m, logResultMsg{
		source: "error",
		lines:  []LogLine{{Time: "02:03:15", Text: "http: proxy error"}},
	})
	if cmd == nil {
		t.Error("log result should reschedule the log poll")
	}
	if m.logSource != "error" || len(m.logs) != 1 || m.logs[0].Text != "http: proxy error" {
		t.Errorf("logs = (%q, %+v)", m.logSource, m.logs)
	}

	m, cmd = update(m, resResultMsg{res: Resources{RAMTotal: 1 << 30}})
	if cmd == nil {
		t.Error("resource result should reschedule the resource poll")
	}
	if m.res.RAMTotal != 1<<30 {
		t.Errorf("res = %+v", m.res)
	}

	m, cmd = update(m, ripple.TickMsg{})
	if cmd == nil {
		t.Error("ripple tick should reschedule the animation")
	}

	// Window resize updates the layout and the rain field size.
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.width != 120 || m.height != 40 {
		t.Errorf("size = %dx%d", m.width, m.height)
	}
	if v := m.View(); !strings.Contains(v, "SERVICE") {
		t.Error("resized view should still render the panel")
	}
}

func TestStateLine(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"gui/501/com.local.llama-swap = {\nstate = running\n}", "state = running"},
		{"state = not running", "state = not running"},
		{"no state line", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := stateLine(tt.in); got != tt.want {
			t.Errorf("stateLine(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in   string
		w    int
		want string
	}{
		{"short", 10, "short"},
		{"exactly ten", 11, "exactly ten"},
		{"a much longer line", 10, "a much lo…"},
		{"ab", 1, "…"},
		{"", 5, ""},
	}
	for _, tt := range tests {
		if got := truncate(tt.in, tt.w); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.w, got, tt.want)
		}
	}
}
