package warlock

// The Bubble Tea dashboard for `llamawizard warlock`: the ripple "rain"
// across the whole terminal with a live stats block centered over it, rain
// showing through the block's spaces — the wizard welcome screen look. The
// engine (engine.go) stays TUI-free; this file is the only place in the
// package that touches tea.

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eduard-lt/llamawizard/internal/launchd"
	"github.com/eduard-lt/llamawizard/internal/ripple"
	"github.com/eduard-lt/llamawizard/internal/state"
)

// Minimum terminal size before the dashboard renders.
const (
	minWidth  = 80
	minHeight = 24
)

// ANSI palette, matching the wizard welcome screen.
const (
	wlRain   = "\x1b[34m"   // base rain blue
	wlPlain  = "\x1b[0m"    // terminal default fg — log/event text
	wlDim    = "\x1b[2m"    // labels, observed timestamps, rules, hints
	wlOK     = "\x1b[32m"   // running / recovered / auto-restart on
	wlWarn   = "\x1b[33m"   // not running / recovering… / death
	wlErr    = "\x1b[31m"   // not loaded / unknown / failed
	wlAccent = "\x1b[1;35m" // bold magenta title
)

// Model is the warlock dashboard: rain across the terminal, stats block
// centered over it.
type Model struct {
	width, height      int
	rip                ripple.Model
	mon                *Monitor
	port               int
	version            string
	localIP            string
	lanHost            string
	logs               []LogLine
	logSource          string
	res                Resources
	upSince, downSince time.Time
}

// InitialModel returns a fresh dashboard for the LaunchAgent at plistPath.
// st may be nil (first run, no state.json): the port then shows as N/A.
// The plist's listen host is read at startup so the dashboard can show
// whether LAN access is open (runWarlock opens it while warlock runs).
func InitialModel(plistPath string, st *state.State, version string) Model {
	port := 0
	if st != nil {
		port = st.Port
	}
	lanHost, _ := launchd.CurrentListenHost(plistPath)
	return Model{
		rip:     ripple.New(),
		mon:     NewMonitor(plistPath),
		port:    port,
		version: version,
		localIP: localIP(),
		lanHost: lanHost,
	}
}

// statusResultMsg carries the outcome of one status poll.
type statusResultMsg struct {
	out string
	err error
}

// restartDoneMsg carries the outcome of a restart attempt.
type restartDoneMsg struct {
	err error
}

// logResultMsg carries the outcome of one log poll.
type logResultMsg struct {
	source string
	lines  []LogLine
}

// resResultMsg carries the outcome of one resource poll.
type resResultMsg struct {
	res Resources
}

// Init sets a short, static terminal title (so the shell's per-command
// title does not clutter the tab) and starts the rain animation and polls.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.SetWindowTitle("llamawarlock"),
		m.rip.Tick(),
		m.statusPoll(),
		m.logPoll(),
		m.resPoll(),
	)
}

// Update handles keys, window resizes, and the poll result messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.mon.SetAutoRestart(!m.mon.AutoRestart())
			return m, nil
		case "a":
			// Ignore the key while a restart is in flight: hammering "a"
			// would otherwise queue duplicate kickstarts and duplicate
			// "restart issued" events.
			if m.mon.InFlight() {
				return m, nil
			}
			m.mon.BeginRestart(time.Now())
			return m, m.restartCmd()
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.rip.SetSize(m.width, m.height)
		return m, nil

	case ripple.TickMsg:
		m.rip.Step()
		return m, m.rip.Tick()

	case statusResultMsg:
		cmd := m.observeStatus(msg)
		return m, tea.Batch(cmd, m.statusPoll())

	case restartDoneMsg:
		m.mon.FinishRestart(msg.err, time.Now())
		return m, nil

	case logResultMsg:
		m.logs = msg.lines
		m.logSource = msg.source
		return m, m.logPoll()

	case resResultMsg:
		m.res = msg.res
		return m, m.resPoll()
	}
	return m, nil
}

// observeStatus classifies a status poll, feeds the monitor, tracks the
// up/down transition times, and issues a restart when the monitor wants one.
func (m *Model) observeStatus(msg statusResultMsg) tea.Cmd {
	s := ClassifyServiceState(msg.out, msg.err)

	var detail string
	if msg.err != nil {
		detail = strings.SplitN(msg.err.Error(), "\n", 2)[0]
	} else {
		if detail = stateLine(msg.out); detail == "" {
			detail = s.String()
		}
	}

	now := time.Now()
	prev := m.mon.State()
	m.mon.Observe(s, detail, now)
	switch {
	case prev != StateRunning && s == StateRunning:
		m.upSince = now
	case prev == StateRunning && s != StateRunning:
		m.downSince = now
	}

	if m.mon.WantsRestart(now) {
		m.mon.BeginRestart(now)
		return m.restartCmd()
	}
	return nil
}

// statusPoll runs the monitor's Status in a goroutine and returns the result.
func (m Model) statusPoll() tea.Cmd {
	mon := m.mon
	return func() tea.Msg {
		out, err := mon.Status()
		return statusResultMsg{out: out, err: err}
	}
}

// restartCmd runs the monitor's Start in a goroutine and returns the result.
func (m Model) restartCmd() tea.Cmd {
	mon := m.mon
	return func() tea.Msg {
		err := mon.Start(mon.PlistPath)
		return restartDoneMsg{err: err}
	}
}

// logPoll fetches the recent log lines in a goroutine and returns them.
func (m Model) logPoll() tea.Cmd {
	return func() tea.Msg {
		home, err := os.UserHomeDir()
		if err != nil {
			return logResultMsg{}
		}
		source, lines := FetchRecentLogs(filepath.Join(home, ".local", "ai", "logs"), 5, time.Now())
		return logResultMsg{source: source, lines: lines}
	}
}

// resPoll snapshots machine resources in a goroutine and returns them.
func (m Model) resPoll() tea.Cmd {
	return func() tea.Msg {
		return resResultMsg{res: SnapshotResources()}
	}
}

// localIP returns the machine's LAN IPv4 address — the one a neighbor on
// the network would use to reach this machine — or "" when it cannot be
// determined. It is resolved once at startup; if the network changes later
// (e.g. Wi-Fi reconnects with a new address), restarting warlock refreshes it.
func localIP() string {
	if ip := egressIP(); ip != "" {
		return ip
	}
	// Fallback: first non-loopback IPv4 on any up interface.
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip4 := ipnet.IP.To4(); ip4 != nil && !ip4.IsLoopback() {
				return ip4.String()
			}
		}
	}
	return ""
}

// egressIP resolves the IP of the interface the default route would use by
// opening a UDP socket toward a public address (8.8.8.8:53, a well-known
// public DNS endpoint). No packets are sent; the kernel only has to pick
// the egress interface.
func egressIP() string {
	conn, err := net.Dial("udp4", "8.8.8.8:53")
	if err != nil {
		return ""
	}
	defer func() { _ = conn.Close() }()
	if ua, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		if ip4 := ua.IP.To4(); ip4 != nil {
			return ip4.String()
		}
	}
	return ""
}

// stateLine returns the "state = …" line from a launchctl print dump, or "".
func stateLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if l := strings.TrimSpace(line); strings.HasPrefix(l, "state = ") {
			return l
		}
	}
	return ""
}

// cell is one terminal cell of the stats block: a rune plus the ANSI
// foreground code to render it with. A zero rune (or a space) is
// transparent — the rain shows through.
type cell struct {
	r  rune
	fg string
}

// seg is a run of text in one foreground color.
type seg struct {
	text string
	fg   string
}

// sline is one line of the stats block: a list of styled segments.
type sline struct{ segs []seg }

// line builds an sline from its styled segments.
func line(segs ...seg) sline { return sline{segs: segs} }

// blockWidth is the stats block width for a terminal of total width: the
// terminal minus 8 columns of rain margin, capped at 64, floored at 40.
func blockWidth(total int) int {
	w := total - 8
	if w > 64 {
		w = 64
	}
	if w < 40 {
		w = 40
	}
	return w
}

// panelLines rebuilds the dashboard's five sections — header, service,
// logs, events, resources — as lines of styled segments, separated by dim
// rules spanning the block width.
func (m Model) panelLines() []sline {
	bw := blockWidth(m.width)
	rule := line(seg{strings.Repeat("─", bw), wlDim})

	auto := "off"
	autoFG := wlDim
	if m.mon.AutoRestart() {
		auto = "on"
		autoFG = wlOK
	}

	var out []sline

	// Header: title + key hints.
	title := "llamawarlock"
	if m.version != "" {
		title += " v" + strings.TrimPrefix(m.version, "v")
	}
	hints := "q quit · r auto-restart [" + auto + "] · a restart now"
	out = append(out,
		line(seg{truncate(title, bw), wlAccent}),
		line(seg{hints, wlDim}),
		rule,
	)

	// Service: state, port, up/down since, auto-restart.
	s := m.mon.State()
	stateWord := s.String()
	var stateFG string
	switch s {
	case StateRunning:
		stateFG = wlOK
	case StateNotLoaded, StateUnknown:
		stateFG = wlErr
	default:
		stateFG = wlWarn
	}
	port := fmt.Sprintf("port %d", m.port)
	if m.port == 0 {
		port = "port N/A"
	}
	svc := []seg{{"SERVICE   ", wlDim}, {"● " + stateWord, stateFG}}
	if s != StateRunning && m.mon.AutoRestart() {
		svc = append(svc, seg{" · ", wlDim}, seg{"recovering…", wlWarn})
	}
	svc = append(svc, seg{" · ", wlDim}, seg{port, wlPlain})
	if m.localIP != "" {
		svc = append(svc, seg{" · ", wlDim}, seg{m.localIP, wlPlain})
	}
	if m.lanHost == "0.0.0.0" {
		svc = append(svc, seg{" · ", wlDim}, seg{"LAN", wlOK})
	}
	out = append(out, line(svc...))
	switch {
	case s == StateRunning && !m.upSince.IsZero():
		out = append(out, line(seg{"up since " + m.upSince.Format("15:04:05"), wlPlain}))
	case s != StateRunning && !m.downSince.IsZero():
		out = append(out, line(seg{"down since " + m.downSince.Format("15:04:05"), wlPlain}))
	}
	out = append(out, line(seg{"auto-restart ", wlDim}, seg{auto, autoFG}), rule)

	// Logs: last 5 lines (or a placeholder).
	source := m.logSource
	if source == "" {
		source = "none"
	}
	out = append(out, line(seg{"LAST 5 LOGS  [" + source + "]", wlDim}))
	if len(m.logs) == 0 {
		out = append(out, line(seg{"  no logs yet", wlDim}))
	} else {
		for _, l := range m.logs {
			tsFG := wlPlain
			if l.Observed {
				tsFG = wlDim
			}
			out = append(out, line(
				seg{"  " + l.Time, tsFG},
				seg{" " + truncate(l.Text, bw-11), wlPlain},
			))
		}
	}
	out = append(out, rule)

	// Events: last 6 monitor events, oldest first.
	out = append(out, line(seg{"EVENTS", wlDim}))
	evs := m.mon.Events()
	if len(evs) > 6 {
		evs = evs[len(evs)-6:]
	}
	if len(evs) == 0 {
		out = append(out, line(seg{"  no events yet", wlDim}))
	} else {
		for _, e := range evs {
			icon, iconFG := eventIcon(e.Kind)
			out = append(out, line(
				seg{"  " + e.Time.Format("15:04:05"), wlDim},
				seg{" " + icon, iconFG},
				seg{" " + truncate(e.Message, bw-13), wlPlain},
			))
		}
	}
	out = append(out, rule)

	// Resources: RAM.
	out = append(out, line(seg{"RESOURCES", wlDim}))
	ram := "N/A"
	if m.res.RAMTotal > 0 {
		used := float64(m.res.RAMTotal-m.res.RAMFree) / (1 << 30)
		total := float64(m.res.RAMTotal) / (1 << 30)
		ram = fmt.Sprintf("%.1f / %.1f GiB", used, total)
	}
	out = append(out, line(seg{"  RAM   ", wlDim}, seg{ram, wlPlain}))
	return out
}

// eventIcon maps an event kind to its display icon and foreground.
func eventIcon(kind string) (string, string) {
	switch kind {
	case "death":
		return "⚠", wlWarn
	case "restart":
		return "↻", wlDim
	case "recovered":
		return "✓", wlOK
	case "failed":
		return "✗", wlErr
	default:
		return "·", wlDim
	}
}

// toBlock expands the panel lines into a grid of cells: each segment's
// runes take the segment's foreground; short lines are padded with zero
// (transparent) cells up to bw. Lines longer than bw keep their full
// length and simply extend into the rain margin.
func toBlock(lines []sline, bw int) [][]cell {
	block := make([][]cell, len(lines))
	for y, l := range lines {
		var row []cell
		for _, s := range l.segs {
			for _, r := range s.text {
				row = append(row, cell{r: r, fg: s.fg})
			}
		}
		for len(row) < bw {
			row = append(row, cell{})
		}
		block[y] = row
	}
	return block
}

// compositeField renders the rain field with the stats block overlaid at
// (fx, fy): block cells with a visible rune are drawn in their own
// foreground, everything else — including the block's spaces — shows the
// rain. Color codes are emitted only when the foreground changes.
func compositeField(field [][]rune, block [][]cell, fx, fy int) string {
	h := len(field)
	w := 0
	if h > 0 {
		w = len(field[0])
	}
	var sb strings.Builder
	for y := 0; y < h; y++ {
		sb.WriteString(wlRain)
		cur := wlRain
		for x := 0; x < w; x++ {
			var c cell
			if y >= fy && y-fy < len(block) && x >= fx && x-fx < len(block[y-fy]) {
				c = block[y-fy][x-fx]
			}
			if c.r != 0 && c.r != ' ' {
				if c.fg != cur {
					sb.WriteString(c.fg)
					cur = c.fg
				}
				sb.WriteRune(c.r)
			} else {
				// A space is invisible, so no color switch is needed for it;
				// skipping the code keeps plain text contiguous across the
				// block's spaces.
				if field[y][x] != ' ' && cur != wlRain {
					sb.WriteString(wlRain)
					cur = wlRain
				}
				sb.WriteRune(field[y][x])
			}
		}
		sb.WriteString("\x1b[0m")
		if y < h-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// View renders the dashboard: the ripple rain across the whole terminal
// with the stats block centered over it, rain showing through the block's
// spaces — the same look as the wizard's welcome screen.
func (m Model) View() string {
	if m.width < minWidth || m.height < minHeight {
		msg := wlDim + "terminal too small — resize to at least 80x24" + wlPlain
		if m.width > 0 && m.height > 0 {
			return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
		}
		return msg
	}
	field := m.rip.Chars(m.width, m.height)
	bw := blockWidth(m.width)
	block := toBlock(m.panelLines(), bw)
	fx, fy := (m.width-bw)/2, (m.height-len(block))/2
	if fx < 0 {
		fx = 0
	}
	if fy < 0 {
		fy = 0
	}
	return compositeField(field, block, fx, fy)
}

// truncate shortens s to w display columns, appending an ellipsis when it
// is cut.
func truncate(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}
