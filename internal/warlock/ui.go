package warlock

// The Bubble Tea dashboard for `llamawizard warlock`: the ripple "rain" in
// the left pane, a live info panel beside it. The engine (engine.go) stays
// TUI-free; this file is the only place in the package that touches tea.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eduard-lt/llamawizard/internal/ripple"
	"github.com/eduard-lt/llamawizard/internal/state"
)

// Poll cadences for the dashboard.
const (
	statusPollInterval = 2 * time.Second
	logPollInterval    = 3 * time.Second
	resPollInterval    = 5 * time.Second
)

// Minimum terminal size before the dashboard renders.
const (
	minWidth  = 80
	minHeight = 24
)

// ANSI colors for the rain, matching the wizard welcome screen.
const (
	rainBlueFG = "\x1b[34m"
	rainReset  = "\x1b[0m"
)

// Palette, mirroring the wizard's adaptive colors.
var (
	wlAccent = lipgloss.AdaptiveColor{Light: "200", Dark: "205"}
	wlMuted  = lipgloss.AdaptiveColor{Light: "240", Dark: "243"}
	wlErr    = lipgloss.AdaptiveColor{Light: "160", Dark: "196"}
	wlOK     = lipgloss.AdaptiveColor{Light: "28", Dark: "46"}
	wlWarn   = lipgloss.AdaptiveColor{Light: "172", Dark: "214"}

	wlHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(wlAccent)
	wlDimStyle    = lipgloss.NewStyle().Foreground(wlMuted)
	wlOKStyle     = lipgloss.NewStyle().Foreground(wlOK)
	wlWarnStyle   = lipgloss.NewStyle().Foreground(wlWarn)
	wlErrStyle    = lipgloss.NewStyle().Foreground(wlErr)
	wlBoxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
)

// Model is the warlock dashboard: rain on the left, info panel on the right.
type Model struct {
	width, height      int
	rip                ripple.Model
	mon                *Monitor
	port               int
	version            string
	logs               []LogLine
	logSource          string
	res                Resources
	gpuModel           string
	upSince, downSince time.Time
}

// InitialModel returns a fresh dashboard for the LaunchAgent at plistPath.
// st may be nil (first run, no state.json): the port then shows as N/A.
func InitialModel(plistPath string, st *state.State, version string) Model {
	port := 0
	if st != nil {
		port = st.Port
	}
	return Model{
		rip:     ripple.New(),
		mon:     NewMonitor(plistPath),
		port:    port,
		version: version,
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

// gpuModelMsg carries the one-time GPU model lookup result.
type gpuModelMsg struct {
	model string
}

// Init starts the rain animation and all the polls.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.rip.Tick(),
		m.statusPoll(),
		m.logPoll(),
		m.resPoll(),
		gpuModelCmd(),
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
			m.mon.BeginRestart(time.Now())
			return m, m.restartCmd()
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.rip.SetSize(rainWidth(m.width), m.height)
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

	case gpuModelMsg:
		m.gpuModel = msg.model
		return m, nil
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
	gpuModel := m.gpuModel
	return func() tea.Msg {
		return resResultMsg{res: SnapshotResources(gpuModel)}
	}
}

// gpuModelCmd looks up the GPU chipset model once at startup.
func gpuModelCmd() tea.Cmd {
	return func() tea.Msg {
		return gpuModelMsg{model: fetchGPUModel()}
	}
}

// fetchGPUModel reads the chipset model from system_profiler, or "" on any
// failure.
func fetchGPUModel() string {
	out, err := exec.Command("system_profiler", "SPDisplaysDataType").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Chipset Model:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Chipset Model:"))
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

// rainWidth is the width of the left rain pane.
func rainWidth(total int) int {
	w := total / 2
	if w < 24 {
		w = 24
	}
	return w
}

// View renders the dashboard: rain on the left, info panel on the right.
func (m Model) View() string {
	if m.width < minWidth || m.height < minHeight {
		msg := wlDimStyle.Render("terminal too small — resize to at least 80x24")
		if m.width > 0 && m.height > 0 {
			return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
		}
		return msg
	}
	rainW := rainWidth(m.width)
	return lipgloss.JoinHorizontal(lipgloss.Top, m.rainView(rainW), m.panelView(rainW))
}

// rainView renders the rain field cell by cell, each row in blue foreground
// exactly like the wizard welcome screen.
func (m Model) rainView(rainW int) string {
	field := m.rip.Chars(rainW, m.height)
	var sb strings.Builder
	for y := 0; y < m.height; y++ {
		sb.WriteString(rainBlueFG)
		for x := 0; x < rainW; x++ {
			sb.WriteRune(field[y][x])
		}
		sb.WriteString(rainReset)
		if y < m.height-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// panelView renders the right-hand info panel: a rounded box with the
// header, service, logs, events, and resources sections.
func (m Model) panelView(rainW int) string {
	inner := m.width - rainW - 2 - 6 // box border (2) + horizontal padding (4)
	if inner < 20 {
		inner = 20
	}

	sections := []string{
		m.headerSection(inner),
		m.serviceSection(),
		m.logsSection(inner),
		m.eventsSection(inner),
		m.resourcesSection(),
	}

	// The dim rule spans the widest content line so the box never stretches
	// beyond its intended width.
	ruleW := inner
	for _, s := range sections {
		for _, line := range strings.Split(s, "\n") {
			if w := lipgloss.Width(line); w > ruleW {
				ruleW = w
			}
		}
	}
	rule := wlDimStyle.Render(strings.Repeat("─", ruleW))

	var sb strings.Builder
	for i, s := range sections {
		if i > 0 {
			sb.WriteString("\n")
			sb.WriteString(rule)
			sb.WriteString("\n")
		}
		sb.WriteString(s)
	}

	return wlBoxStyle.Width(m.width - rainW - 2).Render(sb.String())
}

// headerSection renders the title and the key hints.
func (m Model) headerSection(inner int) string {
	title := "llamawarlock"
	if m.version != "" {
		title += " v" + strings.TrimPrefix(m.version, "v")
	}
	title = truncate(title, inner)
	auto := "off"
	if m.mon.AutoRestart() {
		auto = "on"
	}
	return wlHeaderStyle.Render(title) + "\n" +
		wlDimStyle.Render("q quit · r auto-restart ["+auto+"] · a restart now")
}

// serviceSection renders the service state, port, uptime, and auto-restart.
func (m Model) serviceSection() string {
	s := m.mon.State()
	stateWord := s.String()
	var stateStyle lipgloss.Style
	switch s {
	case StateRunning:
		stateStyle = wlOKStyle
	case StateNotLoaded, StateUnknown:
		stateStyle = wlErrStyle
	default:
		stateStyle = wlWarnStyle
	}

	port := fmt.Sprintf("port %d", m.port)
	if m.port == 0 {
		port = "port N/A"
	}

	line1 := "SERVICE   " + stateStyle.Render("● "+stateWord)
	if s != StateRunning && m.mon.AutoRestart() {
		line1 += " · " + wlWarnStyle.Render("recovering…")
	}
	line1 += " · " + port

	var line2 string
	switch {
	case s == StateRunning && !m.upSince.IsZero():
		line2 = "up since " + m.upSince.Format("15:04:05")
	case s != StateRunning && !m.downSince.IsZero():
		line2 = "down since " + m.downSince.Format("15:04:05")
	}

	auto := "off"
	autoStyle := wlDimStyle
	if m.mon.AutoRestart() {
		auto = "on"
		autoStyle = wlOKStyle
	}
	line3 := "auto-restart " + autoStyle.Render(auto)

	out := line1
	if line2 != "" {
		out += "\n" + line2
	}
	return out + "\n" + line3
}

// logsSection renders the last 5 log lines (or a placeholder).
func (m Model) logsSection(inner int) string {
	source := m.logSource
	if source == "" {
		source = "none"
	}
	out := wlDimStyle.Render("LAST 5 LOGS  [" + source + "]")
	if len(m.logs) == 0 {
		return out + "\n" + wlDimStyle.Render("  no logs yet")
	}
	for _, l := range m.logs {
		ts := l.Time
		if l.Observed {
			ts = wlDimStyle.Render(ts)
		}
		out += "\n  " + ts + " " + truncate(l.Text, inner-11)
	}
	return out
}

// eventsSection renders the last 6 monitor events, oldest first.
func (m Model) eventsSection(inner int) string {
	out := wlDimStyle.Render("EVENTS")
	evs := m.mon.Events()
	if len(evs) > 6 {
		evs = evs[len(evs)-6:]
	}
	if len(evs) == 0 {
		return out + "\n" + wlDimStyle.Render("  no events yet")
	}
	for _, e := range evs {
		icon, style := eventIcon(e.Kind)
		out += "\n  " + wlDimStyle.Render(e.Time.Format("15:04:05")) + " " +
			style.Render(icon) + " " + truncate(e.Message, inner-13)
	}
	return out
}

// eventIcon maps an event kind to its display icon and style.
func eventIcon(kind string) (string, lipgloss.Style) {
	switch kind {
	case "death":
		return "⚠", wlWarnStyle
	case "restart":
		return "↻", wlDimStyle
	case "recovered":
		return "✓", wlOKStyle
	case "failed":
		return "✗", wlErrStyle
	default:
		return "·", wlDimStyle
	}
}

// resourcesSection renders the RAM, CPU, and GPU lines.
func (m Model) resourcesSection() string {
	out := wlDimStyle.Render("RESOURCES")

	ram := "N/A"
	if m.res.RAMTotal > 0 {
		used := float64(m.res.RAMTotal-m.res.RAMFree) / (1 << 30)
		total := float64(m.res.RAMTotal) / (1 << 30)
		ram = fmt.Sprintf("%.1f / %.1f GiB", used, total)
	}
	out += "\n  RAM   " + ram

	cpu := "N/A"
	if m.res.Cores > 0 {
		cpu = fmt.Sprintf("load %.2f (%dc)", m.res.Load1, m.res.Cores)
	}
	out += "\n  CPU   " + cpu

	gpu := m.res.GPUModel
	if gpu == "" {
		gpu = "N/A"
	}
	out += "\n  GPU   " + gpu + " · " + m.res.GPUUtil
	return out
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
