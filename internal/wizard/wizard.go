package wizard

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eduard-lt/llamawizard/internal/build"
	"github.com/eduard-lt/llamawizard/internal/download"
	"github.com/eduard-lt/llamawizard/internal/hardware"
	"github.com/eduard-lt/llamawizard/internal/health"
	"github.com/eduard-lt/llamawizard/internal/launchd"
	"github.com/eduard-lt/llamawizard/internal/llamaswap"
	"github.com/eduard-lt/llamawizard/internal/network"
	"github.com/eduard-lt/llamawizard/internal/pi"
	"github.com/eduard-lt/llamawizard/internal/state"
	"github.com/eduard-lt/llamawizard/internal/whichllm"
)

type Screen int

const (
	ScreenWelcome Screen = iota
	ScreenDeps
	ScreenHardware
	ScreenModelSelect
	ScreenDownload
	ScreenBuild
	ScreenConfig
	ScreenPiSetup
	ScreenPiDefault
	ScreenPort
	ScreenAPIKey
	ScreenLaunchAgent
	ScreenHealth
	ScreenDone
)

var screenOrder = []Screen{ScreenWelcome, ScreenDeps, ScreenHardware, ScreenModelSelect,
	ScreenDownload, ScreenBuild, ScreenConfig, ScreenPiSetup, ScreenPiDefault,
	ScreenPort, ScreenAPIKey, ScreenLaunchAgent, ScreenHealth, ScreenDone}

type ripple struct {
	x, y float64
	r    float64
	maxR float64
	life float64
}

type rippleTickMsg struct{}

var rippleChars = []rune{' ', '.', ':', '-', '=', '+', '*', '#', '%', '@'}

const (
	blueFG       = "\x1b[34m"
	accentFG     = "\x1b[35m"
	boldAccentFG = "\x1b[1;35m"
	infoFG       = "\x1b[36m"
	dimFG        = "\x1b[2m"
	resetFG      = "\x1b[0m"
)

var screenNames = map[Screen]string{
	ScreenWelcome: "Welcome", ScreenDeps: "Dependencies", ScreenHardware: "Hardware",
	ScreenModelSelect: "Model Selection", ScreenDownload: "Download", ScreenBuild: "Build",
	ScreenConfig: "Configuration", ScreenPiSetup: "Pi Setup", ScreenPiDefault: "Pi Default",
	ScreenPort: "Port", ScreenAPIKey: "API Key",
	ScreenLaunchAgent: "LaunchAgent", ScreenHealth: "Health Check", ScreenDone: "Done",
}

var (
	accentAdaptive = lipgloss.AdaptiveColor{Light: "200", Dark: "205"}
	mutedAdaptive  = lipgloss.AdaptiveColor{Light: "240", Dark: "243"}
	errAdaptive    = lipgloss.AdaptiveColor{Light: "160", Dark: "196"}
	okAdaptive     = lipgloss.AdaptiveColor{Light: "28", Dark: "46"}
	infoAdaptive   = lipgloss.AdaptiveColor{Light: "24", Dark: "39"}
	checkAdaptive  = lipgloss.AdaptiveColor{Light: "28", Dark: "82"}
	warnAdaptive   = lipgloss.AdaptiveColor{Light: "172", Dark: "214"}
	fitGoodAdapt   = lipgloss.AdaptiveColor{Light: "28", Dark: "82"}
	fitOkAdapt     = lipgloss.AdaptiveColor{Light: "178", Dark: "214"}
	fitTightAdapt  = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}

	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(accentAdaptive).MarginBottom(1)
	promptStyle  = lipgloss.NewStyle().Foreground(mutedAdaptive).MarginTop(2)
	errorStyle   = lipgloss.NewStyle().Foreground(errAdaptive)
	successIcon  = lipgloss.NewStyle().Foreground(okAdaptive).SetString("✓")
	failureIcon  = lipgloss.NewStyle().Foreground(errAdaptive).SetString("✗")
	infoStyle    = lipgloss.NewStyle().Foreground(infoAdaptive)
	boxStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(accentAdaptive)
	dimStyle     = lipgloss.NewStyle().Foreground(mutedAdaptive)
	checkStyle   = lipgloss.NewStyle().Bold(true).Foreground(checkAdaptive)
	warningStyle = lipgloss.NewStyle().Foreground(warnAdaptive)
	cursorMark   = lipgloss.NewStyle().Bold(true).Foreground(accentAdaptive)

	modelVpHeight = 15
)

func borderFor(success, failure bool) lipgloss.Style {
	s := boxStyle
	if success {
		s = s.BorderForeground(okAdaptive)
	} else if failure {
		s = s.BorderForeground(errAdaptive)
	}
	return s
}

type depsCheckedMsg struct{ statuses []build.DepStatus }

type depsInstalledMsg struct{ err error }

type hwDetectedMsg struct {
	info hardware.HardwareInfo
	err  error
}
type modelsLoadedMsg struct {
	candidates []whichllm.ModelCandidate
	err        error
}
type launchProgressMsg struct {
	step string
	done bool
	err  error
}
type buildStepMsg struct{ step string }
type buildDoneMsg struct {
	llamaCppPath  string
	llamaSwapPath string
	err           error
}
type configGeneratedMsg struct {
	yamlBytes []byte
	err       error
}
type piSetupDoneMsg struct{ err error }
type portCheckMsg struct {
	free         bool
	selfOccupied bool
	alternatives []int
}
type healthCheckMsg struct{ report health.Report }
type dlProgressMsg struct {
	modelIdx   int
	slug       string
	filename   string
	repo       string
	quant      string
	size       int64
	mmproj     string
	downloaded int64
	total      int64
	speedBps   int64
	done       bool
	err        error
}
type dlDoneMsg struct{}

type dlProgEntry struct {
	slug       string
	downloaded int64
	total      int64
	speedBps   int64
	done       bool
	err        error
}

type Model struct {
	Screen Screen
	Width  int
	Height int
	State  *state.State

	spinner        spinner.Model
	deps           []build.DepStatus
	depsDone       bool
	depsInstalling bool
	depsErr        error

	hardware hardware.HardwareInfo
	hwReady  bool

	candidates []whichllm.ModelCandidate
	candLoaded bool
	candErr    error
	selected   map[int]bool
	cursor     int

	dlDone         bool
	dlMsg          string
	dlProgresses   []dlProgEntry
	dlTotal        int
	dlErr          error
	dlCh           chan tea.Msg
	hfTokenFound   bool
	installedSlugs map[string]bool
	dlProgressBars []progress.Model

	buildLog      []string
	buildStep     string
	buildVp       viewport.Model
	modelVp       viewport.Model
	llamaCppPath  string
	llamaSwapPath string

	configYAML []byte
	configDiff string
	configErr  error
	configDone bool

	portInput        textinput.Model
	port             int
	portFree         bool
	portSelfOccupied bool
	portChecked      bool
	portAlts         []int

	keyInput     textinput.Model
	apiKey       string
	setCustomKey bool

	launchDone  bool
	launchErr   error
	launchSteps []string

	healthReport health.Report
	healthDone   bool

	ripples []ripple
	addOnly bool
	version string

	piOptIn       bool
	piSetupDone   bool
	piErr         error
	piDefaultSlug string
	piDefaultIdx  int
}

func InitialModel(version string) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accentAdaptive)

	portTI := textinput.New()
	portTI.Placeholder = "8080"
	portTI.CharLimit = 5
	portTI.Width = 20

	ki := textinput.New()
	ki.Placeholder = "dummy"
	ki.CharLimit = 64
	ki.Width = 40
	ki.EchoMode = textinput.EchoPassword

	buildVp := viewport.New(80, 20)
	modelVp := viewport.New(80, modelVpHeight)

	installedSlugs := make(map[string]bool)
	var existingState *state.State
	if st, err := state.Load(""); err == nil {
		existingState = st
		for _, m := range st.Models {
			installedSlugs[m.Slug] = true
		}
	}

	piOptIn := false
	if existingState != nil && existingState.PiConfigured {
		if pi.IsInstalled() {
			piOptIn = true
		} else {
			existingState.PiConfigured = false
			_ = existingState.Save("")
		}
	}

	m := Model{
		Screen:         ScreenWelcome,
		spinner:        sp,
		portInput:      portTI,
		keyInput:       ki,
		buildVp:        buildVp,
		modelVp:        modelVp,
		selected:       make(map[int]bool),
		port:           8080,
		apiKey:         "dummy",
		installedSlugs: installedSlugs,
		piOptIn:        piOptIn,
		version:        version,
	}

	if existingState != nil && (len(existingState.Models) > 0 || existingState.Port != 0) {
		m.State = existingState
	} else {
		m.State = &state.State{Port: 8080, APIKey: "dummy"}
	}

	return m
}

func InitialAddModel(version string) Model {
	m := InitialModel(version)
	m.addOnly = true
	if st, err := state.Load(""); err == nil && (len(st.Models) > 0 || st.Port != 0) {
		m.State = st
		m.port = st.Port
		if st.APIKey != "" {
			m.apiKey = st.APIKey
		}
		if st.LlamaCppPath != "" {
			m.llamaCppPath = st.LlamaCppPath
		}
		if st.LlamaSwapPath != "" {
			m.llamaSwapPath = st.LlamaSwapPath
		}
		for _, mod := range st.Models {
			m.installedSlugs[mod.Slug] = true
		}
	} else {
		m.State = &state.State{Port: 8080, APIKey: "dummy"}
	}
	if hw, err := hardware.Detect(); err == nil {
		m.hardware = hw
		m.State.Chip = hw.Chip
	}
	m.Screen = ScreenModelSelect
	return m
}

func (m Model) Init() tea.Cmd {
	if m.addOnly {
		return tea.Batch(m.spinner.Tick, rippleTickCmd(), runLoadModels)
	}
	return tea.Batch(m.spinner.Tick, rippleTickCmd())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.buildVp.Width = msg.Width - 8
		m.buildVp.Height = msg.Height - 10
		m.modelVp.Width = msg.Width - 8
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case rippleTickMsg:
		if m.Screen == ScreenWelcome {
			m.updateRipples()
			return m, rippleTickCmd()
		}
		return m, nil

	case progress.FrameMsg:
		if m.Screen == ScreenDownload {
			var cmds []tea.Cmd
			for i := range m.dlProgressBars {
				updated, cmd := m.dlProgressBars[i].Update(msg)
				m.dlProgressBars[i] = updated.(progress.Model)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case depsCheckedMsg:
		m.deps = msg.statuses
		m.depsDone = true
		m.depsErr = nil
		return m, nil

	case depsInstalledMsg:
		m.depsInstalling = false
		if msg.err != nil {
			m.depsErr = msg.err
		}
		m.deps = build.CheckDeps()
		m.depsDone = true
		return m, nil

	case hwDetectedMsg:
		if msg.err == nil {
			m.hardware = msg.info
			m.State.Chip = msg.info.Chip
		}
		m.hwReady = true
		return m, nil

	case modelsLoadedMsg:
		m.candidates = msg.candidates
		m.candLoaded = true
		m.candErr = msg.err
		return m, nil

	case buildStepMsg:
		m.buildStep = msg.step
		return m, nil

	case buildDoneMsg:
		if msg.llamaCppPath != "" {
			m.llamaCppPath = msg.llamaCppPath
		}
		if msg.llamaSwapPath != "" {
			m.llamaSwapPath = msg.llamaSwapPath
		}
		if msg.err != nil {
			m.buildLog = append(m.buildLog, errorStyle.Render("Error: "+msg.err.Error()))
		} else if msg.llamaCppPath != "" {
			m.buildLog = append(m.buildLog, successIcon.String()+" llama.cpp built")
		} else if msg.llamaSwapPath != "" {
			m.buildLog = append(m.buildLog, successIcon.String()+" llama-swap installed")
		}
		m.buildStep = ""
		m.buildVp.SetContent(strings.Join(m.buildLog, "\n"))
		m.buildVp.GotoBottom()
		return m, nil

	case configGeneratedMsg:
		m.configYAML = msg.yamlBytes
		m.configErr = msg.err
		if msg.err == nil {
			_, diff, _ := llamaswap.WriteConfig(state.DefaultConfigPath(), msg.yamlBytes)
			m.configDiff = diff
		}
		return m, nil

	case piSetupDoneMsg:
		m.piSetupDone = true
		if msg.err != nil {
			m.piErr = msg.err
			return m, nil
		}
		m.piErr = nil
		if m.State != nil {
			_ = m.State.Save("")
		}
		m.Screen = ScreenPiDefault
		return m, nil

	case portCheckMsg:
		m.portFree = msg.free
		m.portSelfOccupied = msg.selfOccupied
		m.portChecked = true
		m.portAlts = msg.alternatives
		return m, nil

	case healthCheckMsg:
		m.healthReport = msg.report
		m.healthDone = true
		return m, nil

	case launchProgressMsg:
		if msg.err != nil {
			m.launchErr = msg.err
			m.launchDone = true
			return m, nil
		}
		m.launchSteps = append(m.launchSteps, msg.step)
		if msg.done {
			m.launchDone = true
		}
		return m, nil

	case dlProgressMsg:
		var cmds []tea.Cmd
		if msg.modelIdx >= 0 && msg.modelIdx < len(m.dlProgresses) {
			m.dlProgresses[msg.modelIdx] = dlProgEntry{
				slug:       msg.slug,
				downloaded: msg.downloaded,
				total:      msg.total,
				speedBps:   msg.speedBps,
				done:       msg.done,
				err:        msg.err,
			}
			if msg.total > 0 && msg.modelIdx < len(m.dlProgressBars) {
				pct := float64(msg.downloaded) / float64(msg.total)
				if pct > 1.0 {
					pct = 1.0
				}
				cmds = append(cmds, m.dlProgressBars[msg.modelIdx].SetPercent(pct))
			}
		}
		// Skip slugs already in state (e.g. re-downloaded within this
		// session): a duplicate entry would persist to state.json before
		// GenerateConfig rejects it, breaking every later config
		// regeneration until it is cleaned up.
		if msg.done && msg.err == nil && msg.repo != "" && !m.installedSlugs[msg.slug] {
			m.State.Models = append(m.State.Models, state.ModelEntry{
				Slug:        msg.slug,
				Name:        msg.slug,
				HFRepo:      msg.repo,
				Quant:       msg.quant,
				File:        msg.filename,
				Mmproj:      msg.mmproj,
				SizeBytes:   msg.size,
				InstalledAt: time.Now().Format(time.RFC3339),
			})
			m.installedSlugs[msg.slug] = true
			_ = m.State.Save("")
		} else if msg.done && msg.err != nil {
			if m.dlErr == nil {
				m.dlErr = msg.err
			}
		}
		cmds = append(cmds, dlListen(m.dlCh))
		return m, tea.Batch(cmds...)

	case dlDoneMsg:
		m.dlDone = true
		m.dlMsg = fmt.Sprintf("Downloaded %d models.", len(m.State.Models))
		if m.dlErr != nil {
			m.dlMsg += fmt.Sprintf(" Error: %v", m.dlErr)
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if key == "esc" && m.Screen > ScreenWelcome && m.Screen != ScreenDownload && m.Screen != ScreenBuild {
		m.prevScreen()
		return m, nil
	}

	switch m.Screen {
	case ScreenWelcome:
		if key == "enter" {
			m.Screen = ScreenDeps
			m.ripples = nil
			return m, runCheckDeps
		}
		return m, nil

	case ScreenDeps:
		if key == "o" && m.depsDone {
			m.piOptIn = !m.piOptIn
			return m, nil
		}
		if key == "i" && m.depsDone && !m.depsInstalling {
			if brewPath, brewOk := build.BrewPresent(m.deps); brewOk {
				if installable := build.NeedsBrewInstall(m.deps); len(installable) > 0 {
					m.depsInstalling = true
					m.depsErr = nil
					return m, runInstallDeps(brewPath, installable)
				}
			}
		}
		if key == "enter" && m.depsDone && !m.depsInstalling {
			if build.AllPresent(m.deps) {
				m.Screen = ScreenHardware
				return m, runHardwareDetect
			}
			return m, runCheckDeps
		}

	case ScreenHardware:
		if m.hwReady && (key == "enter" || key == " ") {
			m.Screen = ScreenModelSelect
			return m, runLoadModels
		}

	case ScreenModelSelect:
		return m.handleModelSelect(msg)

	case ScreenDownload:
		if m.dlDone && key == "enter" {
			if len(m.State.Models) == 0 {
				return m, nil
			}
			if m.addOnly {
				m.Screen = ScreenConfig
				return m, runGenerateConfig(m)
			}
			m.Screen = ScreenBuild
			return m, tea.Sequence(
				func() tea.Msg { return buildStepMsg{step: "Building llama.cpp..."} },
				runBuildCpp(m.hardware.Chip),
				func() tea.Msg { return buildStepMsg{step: "Installing llama-swap..."} },
				runBuildSwap(),
			)
		}

	case ScreenBuild:
		if key == "enter" && m.llamaCppPath != "" {
			m.Screen = ScreenConfig
			return m, runGenerateConfig(m)
		}

	case ScreenConfig:
		if key != "enter" {
			return m, nil
		}
		if m.configErr == nil {
			if m.configYAML == nil {
				return m, nil // still generating
			}
			if err := llamaswap.ForceWrite(state.DefaultConfigPath(), m.configYAML); err != nil {
				m.configDiff = errorStyle.Render("Failed to write config: " + err.Error())
				return m, nil
			}
		}
		// When generation failed the write is skipped: the error is already
		// on screen, and the flow continues so the health check can report
		// the missing model instead of leaving the user stuck.
		m.configDone = true

		if m.piOptIn || (m.State != nil && m.State.PiConfigured) {
			m.State.PiConfigured = true
			m.Screen = ScreenPiSetup
			return m, tea.Sequence(
				runPiInstall(),
				runPiConfigureStep(m),
			)
		}

		if m.addOnly {
			m.Screen = ScreenHealth
			return m, tea.Sequence(
				restartServiceCmd(),
				runHealthCheck(m.State.Port, modelIDs(m), m.State.APIKey),
			)
		}
		m.Screen = ScreenPort
		m.portInput.SetValue(fmt.Sprintf("%d", m.port))
		m.portInput.Focus()
		return m, nil

	case ScreenPiSetup:
		if key == "enter" && m.piSetupDone && m.piErr != nil {
			m.piErr = nil
			m.piSetupDone = false
			return m, tea.Sequence(
				runPiInstall(),
				runPiConfigureStep(m),
			)
		}
		if key == "esc" && m.piSetupDone && m.piErr != nil {
			m.piSetupDone = true
			m.piErr = nil
			m.Screen = ScreenPort
			m.portInput.SetValue(fmt.Sprintf("%d", m.port))
			m.portInput.Focus()
			return m, nil
		}

	case ScreenPiDefault:
		if key == "enter" && m.piSetupDone {
			if len(m.State.Models) > 0 {
				m.piDefaultSlug = m.State.Models[m.piDefaultIdx].Slug
			}
			m.Screen = ScreenPort
			m.portInput.SetValue(fmt.Sprintf("%d", m.port))
			m.portInput.Focus()
			return m, nil
		}
		if key == "up" || key == "k" {
			if m.piDefaultIdx > 0 {
				m.piDefaultIdx--
			}
		}
		if key == "down" || key == "j" {
			if m.piDefaultIdx < len(m.State.Models)-1 {
				m.piDefaultIdx++
			}
		}
		return m, nil

	case ScreenPort:
		if key == "enter" {
			m.port = parsePort(m.portInput.Value())
			m.State.Port = m.port
			if m.portChecked && (m.portFree || m.portSelfOccupied) {
				m.Screen = ScreenAPIKey
				return m, nil
			}
			return m, runPortCheck(m.port, m.apiKey)
		}
		var cmd tea.Cmd
		m.portInput, cmd = m.portInput.Update(msg)
		return m, cmd

	case ScreenAPIKey:
		if key == "enter" {
			if m.setCustomKey {
				m.apiKey = m.keyInput.Value()
				if m.apiKey == "" {
					m.apiKey = "dummy"
				}
			}
			m.State.APIKey = m.apiKey
			m.Screen = ScreenLaunchAgent
			return m, runInstallLaunchAgent(m)
		}
		if !m.setCustomKey && key == "y" {
			m.setCustomKey = true
			m.keyInput.Focus()
			return m, nil
		}
		var cmd tea.Cmd
		m.keyInput, cmd = m.keyInput.Update(msg)
		return m, cmd

	case ScreenLaunchAgent:
		if m.launchDone && key == "enter" {
			m.Screen = ScreenHealth
			return m, runHealthCheck(m.port, modelIDs(m), m.apiKey)
		}

	case ScreenHealth:
		if m.healthDone && key == "enter" {
			m.Screen = ScreenDone
			return m, nil
		}

	case ScreenDone:
		if key == "enter" {
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *Model) prevScreen() {
	switch m.Screen {
	case ScreenDeps:
		m.Screen = ScreenWelcome
	case ScreenHardware:
		m.Screen = ScreenDeps
	case ScreenModelSelect:
		m.Screen = ScreenHardware
	case ScreenConfig:
		m.Screen = ScreenBuild
	case ScreenPiSetup:
		m.Screen = ScreenConfig
	case ScreenPiDefault:
		m.Screen = ScreenConfig
	case ScreenPort:
		m.Screen = ScreenConfig
	case ScreenAPIKey:
		m.Screen = ScreenPort
	case ScreenLaunchAgent:
		m.Screen = ScreenAPIKey
	case ScreenHealth:
		m.Screen = ScreenLaunchAgent
	case ScreenDone:
		m.Screen = ScreenHealth
	}
}

var addOnlyScreenOrder = []Screen{ScreenModelSelect, ScreenDownload, ScreenConfig, ScreenHealth, ScreenDone}

func (m Model) breadcrumb() string {
	order := screenOrder
	if m.addOnly {
		order = addOnlyScreenOrder
	}
	dim := lipgloss.NewStyle().Foreground(mutedAdaptive)
	var parts []string
	for _, s := range order {
		if s == m.Screen {
			parts = append(parts, headerStyle.Render(screenNames[s]))
		} else if s < m.Screen && !m.addOnly {
			parts = append(parts, successIcon.String())
		} else {
			parts = append(parts, dim.Render(screenNames[s]))
		}
	}
	return dim.Render(strings.Join(parts, " › "))
}

func (m Model) handleModelSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.candidates)-1 {
			m.cursor++
		}
	case " ":
		if m.cursor >= len(m.candidates) {
			return m, nil
		}
		slug := state.DeriveSlugWithQuant(m.candidates[m.cursor].ModelID, m.candidates[m.cursor].QuantType)
		if !m.installedSlugs[slug] {
			m.selected[m.cursor] = !m.selected[m.cursor]
		}
	case "enter":
		indices := sortedSelIndices(m.selected)
		if len(indices) > 0 {
			m.dlTotal = len(indices)
			m.dlProgresses = make([]dlProgEntry, len(indices))
			m.dlProgressBars = make([]progress.Model, len(indices))
			for i := range m.dlProgressBars {
				m.dlProgressBars[i] = progress.New(
					progress.WithDefaultScaledGradient(),
					progress.WithFillCharacters('█', '░'),
					progress.WithWidth(40),
					progress.WithoutPercentage(),
				)
			}
			m.dlDone = false
			m.dlErr = nil
			m.hfTokenFound = os.Getenv("HF_TOKEN") != "" || os.Getenv("HUGGINGFACE_HUB_TOKEN") != ""

			ch := make(chan tea.Msg, 100)
			m.dlCh = ch
			go downloadAll(ch, indices, m.candidates)

			m.Screen = ScreenDownload
			return m, dlListen(ch)
		}
		if len(m.State.Models) > 0 {
			m.dlDone = true
			if m.addOnly {
				m.Screen = ScreenConfig
				return m, runGenerateConfig(m)
			}
			m.Screen = ScreenBuild
			return m, tea.Sequence(
				func() tea.Msg { return buildStepMsg{step: "Building llama.cpp..."} },
				runBuildCpp(m.hardware.Chip),
				func() tea.Msg { return buildStepMsg{step: "Installing llama-swap..."} },
				runBuildSwap(),
			)
		}
	}
	return m, nil
}

func (m Model) View() string {
	views := map[Screen]func() string{
		ScreenWelcome:     m.welcomeView,
		ScreenDeps:        m.depsView,
		ScreenHardware:    m.hardwareView,
		ScreenModelSelect: m.modelSelectView,
		ScreenDownload:    m.downloadView,
		ScreenBuild:       m.buildView,
		ScreenConfig:      m.configView,
		ScreenPiSetup:     m.piSetupView,
		ScreenPiDefault:   m.piDefaultView,
		ScreenPort:        m.portView,
		ScreenAPIKey:      m.apiKeyView,
		ScreenLaunchAgent: m.launchView,
		ScreenHealth:      m.healthView,
		ScreenDone:        m.doneView,
	}
	if v, ok := views[m.Screen]; ok {
		return m.breadcrumb() + "\n\n" + v()
	}
	return ""
}

var tickDelay = time.Second / 15

func rippleTickCmd() tea.Cmd {
	return tea.Tick(tickDelay, func(t time.Time) tea.Msg {
		return rippleTickMsg{}
	})
}

func (m *Model) updateRipples() {
	if m.Width == 0 || m.Height == 0 {
		return
	}
	rate := 0.15 + rand.Float64()*0.15

	var alive []ripple
	for _, r := range m.ripples {
		r.r += 0.25
		r.life -= 0.015
		if r.life > 0 && r.r < r.maxR {
			alive = append(alive, r)
		}
	}

	if rand.Float64() < rate {
		x := 4 + rand.Float64()*float64(m.Width-8)
		y := 2 + rand.Float64()*float64(m.Height-4)
		maxR := 4 + rand.Float64()*14
		alive = append(alive, ripple{x: x, y: y, r: 0.3, maxR: maxR, life: 1.0})
	}

	m.ripples = alive
}

func (m *Model) rippleCharAt(gx, gy int) rune {
	if len(m.ripples) == 0 {
		return ' '
	}
	best := float64(0.0)
	for _, r := range m.ripples {
		ri := int(r.r)
		if ri <= 0 {
			continue
		}
		dist := math.Sqrt(float64((gx-int(r.x))*(gx-int(r.x))+(gy-int(r.y))*(gy-int(r.y)))) + 0.3
		outerR := float64(ri) + 0.6
		innerR := float64(ri) - 0.8
		if innerR < 0 {
			innerR = 0
		}
		if dist >= innerR && dist <= outerR {
			edge := math.Abs(dist - float64(ri))
			sharp := 1.0 - (edge / 0.8)
			if sharp > 0 {
				v := r.life * sharp
				if v > best {
					best = v
				}
			}
		}
	}
	if best <= 0 {
		return ' '
	}
	ci := int(best * float64(len(rippleChars)-1))
	if ci >= len(rippleChars) {
		ci = len(rippleChars) - 1
	}
	return rippleChars[ci]
}

func (m *Model) renderRipples() string {
	if m.Width == 0 || m.Height == 0 {
		return ""
	}

	hatLines := []string{
		`                                    **##%@%%##              `,
		`                                 +*%%@@%@@@@@@@%            `,
		`                               *@@@@@@@@@      @@           `,
		`                             *#@%%@@@@%          *          `,
		`                            *@@@@%##@@                      `,
		`                           *@@@@@@%@@@                      `,
		`                          *%@@@@@@@@-                       `,
		`                         #%@@%@@@@@@                        `,
		`                        *%@@@@@@@@@@#                       `,
		`                       +%%@ @@@##%@@@                       `,
		`                       %%@  @@ @@@@%@                       `,
		`                      #%@@@     @@%@%                       `,
		`                      ##%@   @   . %#                       `,
		`                     ##@@        *@@@                       `,
		`                     %%@@      @@@@@@@                      `,
		`                    +%%%@@     %@@@@@@@                     `,
		`                   =%@@@@ .  . .@@@%%%@                     `,
		`                   #%@@@@@@@@@@@@@@@@@@@                    `,
		`                  =%%@@@@@@@@@@@@@@@@@@%=                   `,
		`                  -=======--@@@@@@@@@===-                   `,
		`                  =-==--=========-====---                   `,
		`                +====-==--===--===-===-==-                  `,
		`            ++*##%@#@@+===========-%#########.              `,
		`        **#%%%%%%%@@@@@@@@@@@%%%%%%@%%%##########           `,
		`        *%#%%%@@@@@@@@@@@@@@%%%%%%%%@%#######*##%%%         `,
		`           #@@@@%@@@@@@@@@@@@@@%%%@%@%%%%@@                 `,
		`                :%%@@@@@@%%%@@@@@@@@                        `,
	}

	titleLines := []string{
		`██╗     ██╗      █████╗ ███╗   ███╗ █████╗ ██╗    ██╗██╗███████╗ █████╗ ██████╗ ██████╗ `,
		`██║     ██║     ██╔══██╗████╗ ████║██╔══██╗██║    ██║██║╚══███╔╝██╔══██╗██╔══██╗██╔══██╗`,
		`██║     ██║     ███████║██╔████╔██║███████║██║ █╗ ██║██║  ███╔╝ ███████║██████╔╝██║  ██║`,
		`██║     ██║     ██╔══██║██║╚██╔╝██║██╔══██║██║███╗██║██║ ███╔╝  ██╔══██║██╔══██╗██║  ██║`,
		`███████╗███████╗██║  ██║██║ ╚═╝ ██║██║  ██║╚███╔███╔╝██║███████╗██║  ██║██║  ██║██████╔╝`,
		`╚══════╝╚══════╝╚═╝  ╚═╝╚═╝     ╚═╝╚═╝  ╚═╝ ╚══╝╚══╝ ╚═╝╚══════╝╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝ `,
	}

	subtitle := "From zero to a running local LLM stack"
	prompt := "Press Enter to begin"

	var fgBlock []string
	fgBlock = append(fgBlock, titleLines...)
	fgBlock = append(fgBlock, "", subtitle, "")
	fgBlock = append(fgBlock, hatLines...)
	fgBlock = append(fgBlock, "", prompt)

	titleStart := 0
	titleEnd := len(titleLines)
	subIdx := titleEnd + 1
	hatStart := subIdx + 2
	hatEnd := hatStart + len(hatLines)

	if len(m.ripples) == 0 {
		joined := strings.Join(fgBlock, "\n")
		return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, joined)
	}

	fgY := (m.Height - len(fgBlock)) / 2
	if fgY < 0 {
		fgY = 0
	}

	var sb strings.Builder
	for y := 0; y < m.Height; y++ {
		sb.WriteString(blueFG)

		fgIdx := y - fgY
		if fgIdx >= 0 && fgIdx < len(fgBlock) {
			fgLine := fgBlock[fgIdx]
			fgRunes := []rune(fgLine)
			fgWidth := len(fgRunes)
			offset := (m.Width - fgWidth) / 2
			if offset < 0 {
				offset = 0
			}

			for x := 0; x < m.Width; x++ {
				if x >= offset && x < offset+fgWidth {
					ch := fgRunes[x-offset]
					if ch != ' ' {
						sb.WriteString(resetFG)
						if fgIdx >= titleStart && fgIdx < titleEnd {
							sb.WriteString(boldAccentFG)
						} else if fgIdx == subIdx {
							sb.WriteString(infoFG)
						} else if fgIdx >= hatStart && fgIdx < hatEnd {
							sb.WriteString(accentFG)
						} else {
							sb.WriteString(dimFG)
						}
						sb.WriteRune(ch)
						sb.WriteString(blueFG)
						continue
					}
				}
				sb.WriteRune(m.rippleCharAt(x, y))
			}
		} else {
			for x := 0; x < m.Width; x++ {
				sb.WriteRune(m.rippleCharAt(x, y))
			}
		}
		sb.WriteString(resetFG)
		if y < m.Height-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func (m Model) welcomeView() string {
	s := m.renderRipples()
	if m.version != "" {
		v := lipgloss.PlaceHorizontal(m.Width, lipgloss.Right, dimStyle.Render("v"+strings.TrimPrefix(m.version, "v")))
		s += "\n" + v
	}
	return s
}

func (m Model) depsView() string {
	var s string
	if m.depsDone {
		s = "  Checking dependencies... done\n\n"
	} else {
		s = m.spinner.View() + " Checking dependencies...\n\n"
	}
	allOk := true
	someFailed := false
	for _, d := range m.deps {
		icon := "  "
		if m.depsDone {
			if d.Present {
				icon = successIcon.String() + " "
			} else {
				icon = failureIcon.String() + " "
				allOk = false
				someFailed = true
			}
		}
		s += fmt.Sprintf("%s %s\n", icon, d.Name)
	}

	if m.depsDone {
		piInstalled := pi.IsInstalled()
		piIcon := "  [ ] "
		if m.piOptIn {
			piIcon = successIcon.String() + " "
		}
		if piInstalled {
			piIcon = successIcon.String() + " "
		}
		piLabel := "pi (optional)"
		if piInstalled && m.piOptIn {
			piLabel = "pi (configured)"
		} else if piInstalled {
			piLabel = "pi (detected, not configured)"
		}
		s += fmt.Sprintf("%s %s\n", piIcon, piLabel)

		if build.AllPresent(m.deps) {
			s += "\n" + successIcon.Render("All dependencies found.") + "\n"
			if piInstalled && m.piOptIn {
				s += successIcon.Render("pi will be reconfigured with current models") + "\n"
			} else if m.piOptIn {
				s += successIcon.Render("pi will be installed and configured") + "\n"
			} else if piInstalled {
				s += dimStyle.Render("Press 'o' to configure pi with llamawizard") + "\n"
			} else {
				s += dimStyle.Render("Press 'o' to install pi") + "\n"
			}
			s += promptStyle.Render("Press Enter to continue")
		} else if m.depsInstalling {
			var names []string
			for _, d := range build.NeedsBrewInstall(m.deps) {
				names = append(names, d.Name)
			}
			if len(names) > 0 {
				s += "\n" + m.spinner.View() + " Installing: " + strings.Join(names, ", ") + "..."
			} else {
				s += "\n" + m.spinner.View() + " Installing..."
			}
		} else {
			s += "\n" + errorStyle.Render("Some dependencies are missing.") + "\n\n"

			if m.depsErr != nil {
				s += errorStyle.Render("Error: "+m.depsErr.Error()) + "\n\n"
			}

			_, brewOk := build.BrewPresent(m.deps)
			installable := build.NeedsBrewInstall(m.deps)
			hasBrewInstall := brewOk && len(installable) > 0

			if hasBrewInstall {
				var names []string
				for _, d := range installable {
					names = append(names, d.Name)
				}
				s += fmt.Sprintf("  Install with brew: %s\n", infoStyle.Render(strings.Join(names, ", ")))
				s += promptStyle.Render("  Press i to install") + "\n\n"
			}

			for _, d := range m.deps {
				if d.Missing() {
					s += fmt.Sprintf("  %s:  %s\n", warningStyle.Render(d.Name), build.InstallCommand(d.Name))
				}
			}
			s += "\n" + promptStyle.Render("Press Enter to re-check")
		}
	}
	return borderFor(allOk && m.depsDone, someFailed).Width(m.Width - 4).Render(s)
}

func (m Model) hardwareView() string {
	s := "Detecting hardware...\n\n"
	ready := false
	if m.hwReady {
		ready = true
		s += fmt.Sprintf("  Chip: %s\n", infoStyle.Render(m.hardware.Chip))
		s += fmt.Sprintf("  Model: %s\n", m.hardware.Model)
		s += fmt.Sprintf("  RAM: %d GB\n", m.hardware.RAM/(1024*1024*1024))
		if m.hardware.Metal {
			s += fmt.Sprintf("  Metal: %s\n", successIcon.Render("available"))
		} else {
			s += fmt.Sprintf("  Metal: %s\n", failureIcon.String()+" unavailable")
		}
		s += "\n" + promptStyle.Render("Press Enter to continue")
	} else {
		s += m.spinner.View()
	}
	return borderFor(ready, false).Width(m.Width - 4).Render(s)
}

func (m Model) modelSelectView() string {
	if !m.candLoaded {
		return boxStyle.Width(m.Width - 4).Render(m.spinner.View() + " Loading model recommendations...")
	}
	if m.candErr != nil {
		return boxStyle.Width(m.Width - 4).Render(errorStyle.Render("Error: " + m.candErr.Error()))
	}
	if len(m.candidates) == 0 {
		return boxStyle.Width(m.Width - 4).Render("No models found.")
	}

	var b strings.Builder
	b.WriteString(dimStyle.Render("space toggle   enter confirm   ↑↓ navigate"))
	b.WriteString("\n\n")

	header := "    " + padCell("Model", 27) + padCell("Quant", 9) +
		padCell("Size", 9) + padCell("tok/s", 7) + padCell("Fit", 12)
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	for i, c := range m.candidates {
		slug := state.DeriveSlugWithQuant(c.ModelID, c.QuantType)
		installed := m.installedSlugs[slug]
		selected := m.selected[i]
		isCursor := i == m.cursor

		cursor := "  "
		if isCursor {
			cursor = cursorMark.Render("› ")
		}

		mark := " "
		switch {
		case selected:
			mark = checkStyle.Render("✓")
		case installed:
			mark = dimStyle.Render("i")
		}

		modelID := shortModelID(c.ModelID)
		sizeStr := fmt.Sprintf("%.1fG", float64(c.FileSizeBytes)/(1024*1024*1024))
		tokStr := fmt.Sprintf("%.0f", c.EstimatedTokPerSec)

		nameCell := padCell(modelID, 27)
		quantCell := padCell(c.QuantType, 9)
		sizeCell := padCell(sizeStr, 9)
		tokCell := padCell(tokStr, 7)
		fitCell := fitStyleFor(c.FitType).Render(padCell(c.FitType, 12))

		if installed {
			row := cursor + mark + " " + dimStyle.Render(nameCell+quantCell+sizeCell+tokCell+padCell(c.FitType, 12))
			b.WriteString(row + dimStyle.Render("  installed") + "\n")
			continue
		}

		b.WriteString(cursor + mark + " " + nameCell + quantCell + sizeCell + tokCell + fitCell + "\n")
	}

	b.WriteString("\n")
	fmt.Fprintf(&b, "%d selected  ", len(selIndices(m.selected)))
	b.WriteString(checkStyle.Render("✓") + dimStyle.Render(" selected  ") +
		dimStyle.Render("i") + dimStyle.Render(" already installed"))
	if len(selIndices(m.selected)) == 0 && len(m.State.Models) > 0 {
		b.WriteString("\n\n" + promptStyle.Render("Press Enter to continue without downloading new models"))
	}

	return boxStyle.Width(m.Width - 4).Render(b.String())
}

func padCell(s string, w int) string {
	runes := []rune(s)
	if len(runes) > w {
		return string(runes[:w-1]) + "…"
	}
	return s + strings.Repeat(" ", w-len([]rune(s)))
}

func fitStyleFor(fit string) lipgloss.Style {
	switch strings.ToLower(fit) {
	case "full_gpu", "full gpu", "excellent_fit", "excellent fit", "fits":
		return lipgloss.NewStyle().Foreground(fitGoodAdapt)
	case "tight_fit", "tight fit", "tight", "partial_gpu":
		return lipgloss.NewStyle().Foreground(fitTightAdapt)
	default:
		return lipgloss.NewStyle().Foreground(fitOkAdapt)
	}
}

func (m Model) downloadView() string {
	s := "Downloading models...\n"
	if m.hfTokenFound {
		s += "  " + infoStyle.Render("HF token: found (authenticated)") + "\n"
	} else {
		s += "  " + promptStyle.Render("HF token: not set (slower downloads)") + "\n"
	}
	s += "\n\n"
	for i, p := range m.dlProgresses {
		if i >= m.dlTotal {
			break
		}
		name := p.slug
		if name == "" {
			name = fmt.Sprintf("model %d", i+1)
		}
		if p.err != nil {
			s += "    " + headerStyle.Render(name) + "\n"
			s += "    " + errorStyle.Render("FAILED: "+p.err.Error()) + "\n"
		} else if p.done {
			s += "    " + headerStyle.Render(name) + "\n"
			s += "    " + successIcon.Render("Done") + "\n"
		} else if p.total > 0 && i < len(m.dlProgressBars) {
			bar := m.dlProgressBars[i].View()
			pct := 0
			if p.total > 0 {
				pct = int(float64(p.downloaded) / float64(p.total) * 100)
			}
			sizeStr := fmt.Sprintf("%.1f/%.1f GB", float64(p.downloaded)/(1<<30), float64(p.total)/(1<<30))
			speedStr := ""
			if p.speedBps > 0 {
				speedStr = fmt.Sprintf("  %.1f MB/s", float64(p.speedBps)/(1024*1024))
			}
			s += "    " + headerStyle.Render(name) + "\n"
			s += fmt.Sprintf("    %s %3d%%  %s%s\n", bar, pct, sizeStr, speedStr)
		} else {
			s += "    " + headerStyle.Render(name) + "\n"
			s += "    " + m.spinner.View() + " queued...\n"
		}
	}
	if m.dlDone {
		if len(m.State.Models) > 0 {
			s += "\n" + successIcon.Render(fmt.Sprintf("%d model(s) installed.", len(m.State.Models))) + "\n"
		}
		if m.dlErr != nil {
			s += errorStyle.Render(m.dlErr.Error()) + "\n"
		}
		s += "\n" + promptStyle.Render("Press Enter to continue")
	} else {
		s += "\n" + m.spinner.View() + " Downloading..."
	}
	return borderFor(m.dlDone && m.dlErr == nil, m.dlDone && m.dlErr != nil).Width(m.Width - 4).Render(s)
}

func (m Model) buildView() string {
	m.buildVp.SetContent(strings.Join(m.buildLog, "\n"))
	header := "Building llama.cpp + installing llama-swap...\n"
	if m.llamaCppPath == "" {
		header += "(this may take several minutes)\n\n"
	} else {
		header += "\n"
	}
	if m.buildStep != "" {
		header += m.spinner.View() + " " + m.buildStep + "\n"
	}
	s := header + m.buildVp.View()
	if m.llamaCppPath != "" {
		s += "\n" + promptStyle.Render("Press Enter to continue")
	}
	done := m.llamaCppPath != "" && m.llamaSwapPath != ""
	return borderFor(done, false).Width(m.Width - 4).Render(s)
}

func (m Model) configView() string {
	s := "Generating llama-swap configuration...\n\n"
	ready := false
	hasConflict := false
	if m.configErr != nil {
		s += errorStyle.Render(m.configErr.Error()) + "\n\n"
		s += promptStyle.Render("Press Enter to continue without writing the configuration.")
		return borderFor(false, true).Width(m.Width - 4).Render(s)
	}
	if m.configYAML != nil {
		ready = !m.configDone
		hasConflict = m.configDiff != ""
		s += successIcon.Render("Configuration generated.") + "\n\n"
		preview := string(m.configYAML)
		if len(preview) > 400 {
			preview = preview[:400] + "\n..."
		}
		s += dimStyle.Render(preview)
		if m.configDiff != "" {
			s += "\n\n" + lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "178", Dark: "226"}).Render("Diff:\n"+m.configDiff)
		}
		s += "\n" + promptStyle.Render("Press Enter to save and continue")
	} else {
		s += m.spinner.View()
	}
	return borderFor(ready && !hasConflict, hasConflict).Width(m.Width - 4).Render(s)
}

func (m Model) portView() string {
	s := "Choose a port for llama-swap:\n\n"
	s += m.portInput.View() + "\n\n"
	valid := false
	invalid := false
	if m.portChecked && m.port == parsePort(m.portInput.Value()) {
		if m.portFree {
			valid = true
			if m.portSelfOccupied {
				s += successIcon.Render(fmt.Sprintf("Port %d is in use by your existing llama-swap — this is fine.", m.port))
			} else {
				s += successIcon.Render(fmt.Sprintf("Port %d is available.", m.port))
			}
			s += "\n" + promptStyle.Render("Press Enter to proceed")
		} else {
			invalid = true
			s += errorStyle.Render(fmt.Sprintf("Port %d is in use.", m.port))
			if len(m.portAlts) > 0 {
				s += fmt.Sprintf("\n\nAlternatives: %v", intSliceStr(m.portAlts))
			}
			s += "\n" + promptStyle.Render("Type another port and press Enter")
		}
	} else {
		s += promptStyle.Render("Type a port and press Enter")
	}
	return borderFor(valid, invalid).Width(m.Width - 4).Render(s)
}

func (m Model) apiKeyView() string {
	s := "Set an API key for llama-swap:\n\n"
	if m.setCustomKey {
		s += "Enter your API key:\n\n"
		s += m.keyInput.View()
		s += "\n\n" + promptStyle.Render("Press Enter to confirm")
	} else {
		s += fmt.Sprintf("Default: %s\n\n", infoStyle.Render("dummy"))
		s += promptStyle.Render("Press Enter for dummy, or Y to set a custom key")
	}
	return boxStyle.Width(m.Width - 4).Render(s)
}

func (m Model) launchView() string {
	s := "Installing LaunchAgent service...\n\n"
	done := false
	failed := false
	if m.launchDone {
		if m.launchErr != nil {
			failed = true
			s += errorStyle.Render("Error: " + m.launchErr.Error())
		} else {
			done = true
			s += "  " + successIcon.String() + " Wrote plist\n"
			s += "  " + successIcon.String() + " Registered with launchd\n"
			s += "\n" + successIcon.Render("Service installed and running.") + "\n"
			s += fmt.Sprintf("Plist: %s\n", infoStyle.Render("~/Library/LaunchAgents/com.local.llama-swap.plist"))
			s += "\nThe service will auto-start on next login."
		}
		s += "\n" + promptStyle.Render("Press Enter to continue")
	} else {
		s += m.spinner.View() + " Working...\n\n"
		steps := []string{"Writing LaunchAgent plist", "Registering with launchd"}
		for i, step := range steps {
			icon := "  - "
			if i < len(m.launchSteps) {
				icon = "  " + successIcon.String() + " "
			}
			s += icon + step + "\n"
		}
	}
	return borderFor(done, failed).Width(m.Width - 4).Render(s)
}

func (m Model) piSetupView() string {
	s := "Setting up pi...\n\n"
	if m.piSetupDone {
		if m.piErr != nil {
			s += errorStyle.Render("Pi setup failed: "+m.piErr.Error()) + "\n\n"
			s += promptStyle.Render("Press Enter to retry, Esc to skip")
		} else {
			s += successIcon.Render("Pi configured successfully.") + "\n"
		}
	} else {
		s += m.spinner.View() + " Installing and configuring pi..."
	}
	return borderFor(m.piSetupDone && m.piErr == nil, m.piSetupDone && m.piErr != nil).Width(m.Width - 4).Render(s)
}

func (m Model) piDefaultView() string {
	s := "Choose a default model for pi:\n\n"
	for i, mdl := range m.State.Models {
		cursor := "  "
		if i == m.piDefaultIdx {
			cursor = cursorMark.Render("› ")
		}
		id := mdl.Slug
		if mdl.Name != "" {
			id = mdl.Name + " (" + mdl.Slug + ")"
		}
		s += cursor + id + "\n"
	}
	s += "\n" + promptStyle.Render("↑↓ navigate   Enter to confirm")
	return borderFor(false, false).Width(m.Width - 4).Render(s)
}

func (m Model) healthView() string {
	s := "Running health check...\n\n"
	passed := false
	failed := false
	if m.healthDone {
		r := m.healthReport
		if r.Pass {
			passed = true
			s += successIcon.Render("All models loaded and healthy!") + "\n"
		} else {
			failed = true
			s += errorStyle.Render("Health check failed.") + "\n"
			if r.Error != "" {
				s += fmt.Sprintf("Error: %s\n", r.Error)
			}
			if len(r.MissingModels) > 0 {
				s += fmt.Sprintf("Missing: %v\n", r.MissingModels)
			}
			if r.ErrorLogTail != "" {
				s += fmt.Sprintf("\nError log tail:\n%s\n", r.ErrorLogTail)
			}
		}
		s += fmt.Sprintf("\nFound: %d models  Attempts: %d  Duration: %s\n",
			len(r.FoundModels), r.Attempts, r.Duration)
		s += "\n" + promptStyle.Render("Press Enter to continue")
	} else {
		s += m.spinner.View() + " Polling /v1/models..."
	}
	return borderFor(passed, failed).Width(m.Width - 4).Render(s)
}

func (m Model) doneView() string {
	s := titleStyle.Render("Setup Complete!") + "\n\n"
	s += fmt.Sprintf("  Port:        %d\n", m.port)
	s += fmt.Sprintf("  API Key:     %s\n", m.apiKey)
	s += fmt.Sprintf("  llama.cpp:   %s\n", infoStyle.Render(m.llamaCppPath))
	s += fmt.Sprintf("  llama-swap:  %s\n", infoStyle.Render(m.llamaSwapPath))
	s += "  Config:      ~/.local/ai/config/llama-swap.yaml\n"
	s += fmt.Sprintf("  Models:      %d installed\n", len(m.State.Models))
	s += fmt.Sprintf("\n  Open: http://localhost:%d/v1/models to verify.\n", m.port)
	s += "\n" + promptStyle.Render("Press Enter to exit")
	return borderFor(true, false).Width(m.Width - 4).Render(s)
}

func runCheckDeps() tea.Msg {
	return depsCheckedMsg{statuses: build.CheckDeps()}
}

func runInstallDeps(brewPath string, deps []build.DepStatus) tea.Cmd {
	return func() tea.Msg {
		return depsInstalledMsg{err: build.AutoInstallBrew(brewPath, deps)}
	}
}

func runHardwareDetect() tea.Msg {
	info, err := hardware.Detect()
	return hwDetectedMsg{info: info, err: err}
}

func runLoadModels() tea.Msg {
	candidates, err := whichllm.Rank("general", 15)
	return modelsLoadedMsg{candidates: candidates, err: err}
}

func runBuildCpp(chip string) tea.Cmd {
	return func() tea.Msg {
		path, err := build.EnsureLlamaCpp(chip)
		return buildDoneMsg{llamaCppPath: path, err: err}
	}
}

func runBuildSwap() tea.Cmd {
	return func() tea.Msg {
		path, err := llamaswap.EnsureLlamaSwap()
		return buildDoneMsg{llamaSwapPath: path, err: err}
	}
}

func runGenerateConfig(m Model) tea.Cmd {
	return func() tea.Msg {
		models := selectedModels(m)
		yamlBytes, err := llamaswap.GenerateConfig(models, m.apiKey, m.llamaCppPath, m.hardware)
		return configGeneratedMsg{yamlBytes: yamlBytes, err: err}
	}
}

func runPortCheck(port int, apiKey string) tea.Cmd {
	return func() tea.Msg {
		free := network.IsFree(port)
		selfOccupied := false
		if !free {
			selfOccupied = network.IsLlamaSwapPort(port, apiKey)
		}
		alts := network.SuggestAlternatives(port, 3)
		return portCheckMsg{free: free || selfOccupied, selfOccupied: selfOccupied, alternatives: alts}
	}
}

func restartServiceCmd() tea.Cmd {
	return func() tea.Msg {
		_ = launchd.Stop()
		home, _ := os.UserHomeDir()
		plistPath := filepath.Join(home, "Library", "LaunchAgents", launchd.PlistName)
		_ = launchd.Start(plistPath)
		return nil
	}
}

func runInstallLaunchAgent(m Model) tea.Cmd {
	plistPath := ""
	configPath := state.DefaultConfigPath()

	return tea.Sequence(
		func() tea.Msg {
			path, err := launchd.WritePlist(m.llamaSwapPath, configPath, m.port)
			if err != nil {
				return launchProgressMsg{done: true, err: err}
			}
			plistPath = path
			return launchProgressMsg{step: "Wrote plist"}
		},
		func() tea.Msg {
			err := launchd.Install(plistPath)
			if err != nil {
				return launchProgressMsg{done: true, err: err}
			}
			return launchProgressMsg{step: "Registered with launchd", done: true}
		},
	)
}

func runHealthCheck(port int, modelIDs []string, apiKey string) tea.Cmd {
	return func() tea.Msg {
		r, err := health.CheckWithKey(port, modelIDs, apiKey)
		if err != nil {
			return healthCheckMsg{report: health.Report{Error: err.Error(), Pass: false}}
		}
		return healthCheckMsg{report: r}
	}
}

func parsePort(s string) int {
	var p int
	_, _ = fmt.Sscanf(s, "%d", &p)
	if p < 1 || p > 65535 {
		p = 8080
	}
	return p
}

func selIndices(sel map[int]bool) []int {
	var idx []int
	for i, ok := range sel {
		if ok {
			idx = append(idx, i)
		}
	}
	return idx
}

func selectedModels(m Model) []state.ModelEntry {
	if len(m.State.Models) > 0 {
		return m.State.Models
	}
	var models []state.ModelEntry
	for _, i := range sortedSelIndices(m.selected) {
		if i < len(m.candidates) {
			c := m.candidates[i]
			repo := c.ModelID
			filename := c.ModelID + "-" + c.QuantType + ".gguf"
			if c.ArtifactRepoID != nil && *c.ArtifactRepoID != "" {
				repo = *c.ArtifactRepoID
			}
			if c.ArtifactFilename != nil && *c.ArtifactFilename != "" {
				filename = *c.ArtifactFilename
			}
			models = append(models, state.ModelEntry{
				Slug:      state.DeriveSlugWithQuant(c.ModelID, c.QuantType),
				Name:      c.ModelID,
				HFRepo:    repo,
				Quant:     c.QuantType,
				File:      filename,
				SizeBytes: c.FileSizeBytes,
			})
		}
	}
	return models
}

func modelIDs(m Model) []string {
	var ids []string
	for _, mdl := range m.State.Models {
		ids = append(ids, mdl.Slug)
	}
	if len(ids) == 0 {
		for _, sm := range selectedModels(m) {
			ids = append(ids, sm.Slug)
		}
	}
	return ids
}

func sortedSelIndices(sel map[int]bool) []int {
	indices := selIndices(sel)
	for i := 0; i < len(indices)-1; i++ {
		for j := i + 1; j < len(indices); j++ {
			if indices[j] < indices[i] {
				indices[i], indices[j] = indices[j], indices[i]
			}
		}
	}
	return indices
}

func dlListen(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return dlDoneMsg{}
		}
		return msg
	}
}

func resolveRepos(c whichllm.ModelCandidate) []string {
	var repos []string
	if c.ArtifactRepoID != nil && *c.ArtifactRepoID != "" {
		repos = append(repos, *c.ArtifactRepoID)
	}
	shortName := shortModelID(c.ModelID)
	publisher := ""
	if idx := strings.LastIndex(c.ModelID, "/"); idx >= 0 {
		publisher = c.ModelID[:idx]
	}
	for _, prefix := range []string{"ggml-org", "unsloth", "bartowski", "mlx-community"} {
		repos = append(repos, fmt.Sprintf("%s/%s-GGUF", prefix, shortName))
	}
	if publisher != "" {
		repos = append(repos, fmt.Sprintf("%s/%s-GGUF", publisher, shortName))
	}
	repos = append(repos, c.ModelID)
	return repos
}

func downloadAll(ch chan<- tea.Msg, indices []int, candidates []whichllm.ModelCandidate) {
	defer close(ch)

	const progressThrottle = 250 * time.Millisecond

	for i, idx := range indices {
		c := candidates[idx]

		repos := resolveRepos(c)
		var files []download.RemoteFile
		var resolvedRepo string
		var lastErr error
		var tried []string
		for _, repo := range repos {
			files, lastErr = download.ResolveFiles(repo, c.QuantType)
			if lastErr == nil {
				resolvedRepo = repo
				break
			}
			tried = append(tried, repo)
		}
		if lastErr != nil {
			err := fmt.Errorf("tried %d repos for quant %q (last: %s): %w",
				len(tried), c.QuantType, tried[len(tried)-1], lastErr)
			ch <- dlProgressMsg{modelIdx: i, err: err, done: true}
			continue
		}

		var mainFile, mmprojFile string
		var combinedTotal int64
		for _, f := range files {
			combinedTotal += f.Size
			if f.IsMmproj {
				mmprojFile = f.Filename
			} else {
				mainFile = f.Filename
			}
		}

		allOk := true
		var combinedDownloaded int64
		for _, f := range files {
			home, _ := os.UserHomeDir()
			slug := state.DeriveSlugWithQuant(c.ModelID, c.QuantType)
			destDir := filepath.Join(home, "models", slug)

			progCh := make(chan download.Progress, 20)
			errCh := make(chan error, 1)

			offsetBefore := combinedDownloaded

			go func() {
				errCh <- download.Download(f, destDir, progCh)
				close(progCh)
			}()

			var lastSent time.Time
			for p := range progCh {
				combined := offsetBefore + p.Downloaded
				now := time.Now()
				last := p.Downloaded >= p.Total && p.Total > 0
				if last || now.Sub(lastSent) >= progressThrottle {
					ch <- dlProgressMsg{
						modelIdx:   i,
						slug:       slug,
						filename:   p.Filename,
						downloaded: combined,
						total:      combinedTotal,
						speedBps:   p.BytesPerSec,
					}
					lastSent = now
				}
			}

			if err := <-errCh; err != nil {
				ch <- dlProgressMsg{modelIdx: i, slug: slug, err: err, done: true}
				allOk = false
				break
			}
			combinedDownloaded += f.Size
		}

		if allOk {
			ch <- dlProgressMsg{
				modelIdx: i,
				slug:     state.DeriveSlugWithQuant(c.ModelID, c.QuantType),
				filename: mainFile,
				repo:     resolvedRepo,
				quant:    c.QuantType,
				size:     c.FileSizeBytes,
				mmproj:   mmprojFile,
				done:     true,
			}
		}
	}

	ch <- dlDoneMsg{}
}

func intSliceStr(s []int) string {
	strs := make([]string, len(s))
	for i, v := range s {
		strs[i] = fmt.Sprintf("%d", v)
	}
	return strings.Join(strs, ", ")
}

func shortModelID(full string) string {
	if idx := strings.LastIndex(full, "/"); idx >= 0 {
		return full[idx+1:]
	}
	return full
}

func runPiInstall() tea.Cmd {
	return func() tea.Msg {
		if pi.IsInstalled() {
			return piSetupDoneMsg{}
		}
		if err := pi.Install(); err != nil {
			return piSetupDoneMsg{err: err}
		}
		return piSetupDoneMsg{}
	}
}

func runPiConfigureStep(m Model) tea.Cmd {
	return func() tea.Msg {
		models := selectedModels(m)
		if len(models) == 0 {
			models = m.State.Models
		}
		defaultModel := m.piDefaultSlug
		if defaultModel == "" && len(models) > 0 {
			defaultModel = models[0].Slug
		}
		if err := pi.ConfigureModels(m.State.Port, models); err != nil {
			return piSetupDoneMsg{err: err}
		}
		if err := pi.ConfigureSettings(defaultModel, models); err != nil {
			return piSetupDoneMsg{err: err}
		}
		return piSetupDoneMsg{}
	}
}
