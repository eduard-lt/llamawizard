package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eduard-lt/llamawizard/internal/download"
)

// linkScreen enumerates the phases of the add-from-link TUI.
type linkScreen int

const (
	lsPick linkScreen = iota
	lsDownload
	lsDone
	lsError
)

// linkDlMsg is a progress update from the download goroutine.
type linkDlMsg struct {
	downloaded int64
	total      int64
	speed      int64
	filename   string
	done       bool
	err        error
}

// linkAddResult is what the picker TUI reports back after running.
type linkAddResult struct {
	main   download.RemoteFile
	mmproj *download.RemoteFile
	slug   string
	ok     bool
}

var (
	lpCursor = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	lpGood   = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
)

// linkPicker is the TUI for picking a GGUF file from a repo and downloading it
// with a live progress bar, percentage, and speed.
type linkPicker struct {
	repo      string
	name      string
	mains     []download.RemoteFile
	mmprojs   []download.RemoteFile
	preselect bool

	screen linkScreen
	cursor int

	main   download.RemoteFile
	mmproj *download.RemoteFile
	slug   string

	progCh     chan tea.Msg
	downloaded int64
	total      int64
	speedBps   int64
	current    string
	bar        progress.Model
	dlErr      error

	result linkAddResult

	width  int
	height int
}

func (m *linkPicker) Init() tea.Cmd {
	if m.preselect {
		return m.startDownload()
	}
	return nil
}

func (m *linkPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case progress.FrameMsg:
		if m.screen == lsDownload {
			updated, cmd := m.bar.Update(msg)
			m.bar = updated.(progress.Model)
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.screen {
		case lsPick:
			switch msg.String() {
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.mains)-1 {
					m.cursor++
				}
			case "enter":
				m.main = m.mains[m.cursor]
				m.mmproj = pickMmproj(m.mmprojs, m.main.Filename)
				m.slug = deriveSlug(m.name, m.main.Filename)
				m.bar = newLinkBar()
				m.screen = lsDownload
				return m, m.startDownload()
			case "esc":
				return m, tea.Quit
			}
		case lsDone, lsError:
			if msg.String() == "enter" || msg.String() == "esc" {
				return m, tea.Quit
			}
		}
		return m, nil

	case linkDlMsg:
		if msg.err != nil {
			m.dlErr = msg.err
			m.screen = lsError
			return m, nil
		}

		m.downloaded = msg.downloaded
		m.total = msg.total
		m.speedBps = msg.speed
		m.current = msg.filename

		var cmds []tea.Cmd
		if m.total > 0 {
			pct := float64(m.downloaded) / float64(m.total)
			if pct > 1.0 {
				pct = 1.0
			}
			cmds = append(cmds, m.bar.SetPercent(pct))
		}

		if msg.done {
			m.result = linkAddResult{main: m.main, mmproj: m.mmproj, slug: m.slug, ok: true}
			m.screen = lsDone
			return m, nil
		}

		cmds = append(cmds, linkDlListen(m.progCh))
		return m, tea.Batch(cmds...)
	}

	return m, nil
}

func (m *linkPicker) View() string {
	switch m.screen {
	case lsPick:
		return m.pickView()
	case lsDownload:
		return m.downloadView()
	case lsDone:
		return m.doneView()
	case lsError:
		return m.errorView()
	}
	return ""
}

func (m *linkPicker) pickView() string {
	var b strings.Builder
	b.WriteString(ltTitle.Render("Pick a GGUF file"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "Found %d file(s) in %s:\n", len(m.mains), ltCode.Render(m.repo))

	// Each entry renders as two lines (filename + size). Reserve space for the
	// title, "Found" line, mmproj note, hints, and box border/padding.
	overhead := 14
	if len(m.mmprojs) == 0 {
		overhead = 12
	}
	maxFiles := (m.height - overhead) / 2
	if maxFiles < 1 {
		maxFiles = 1
	}

	start := m.cursor - maxFiles + 1
	if start < 0 {
		start = 0
	}
	end := start + maxFiles
	if end > len(m.mains) {
		end = len(m.mains)
		start = end - maxFiles
		if start < 0 {
			start = 0
		}
	}

	if start > 0 {
		b.WriteString(ltDim.Render(fmt.Sprintf("  … %d more above\n", start)))
	}
	for i := start; i < end; i++ {
		f := m.mains[i]
		cursor := "  "
		if i == m.cursor {
			cursor = lpCursor.Render("› ")
		}
		fmt.Fprintf(&b, "%s %s\n", cursor, f.Filename)
		fmt.Fprintf(&b, "     %s\n", ltDim.Render(humanSize(f.Size)))
	}
	if end < len(m.mains) {
		b.WriteString(ltDim.Render(fmt.Sprintf("  … %d more below\n", len(m.mains)-end)))
	}

	if len(m.mmprojs) > 0 {
		b.WriteString("\n" + ltDim.Render("A multimodal projector (mmproj) is included automatically."))
	}
	b.WriteString("\n" + ltDim.Render("↑↓ navigate   enter select   esc cancel"))
	return ltBox.Render(b.String())
}

func (m *linkPicker) downloadView() string {
	var b strings.Builder
	b.WriteString(ltTitle.Render("Downloading"))
	b.WriteString("\n\n")

	name := m.current
	if name == "" {
		name = m.main.Filename
	}
	b.WriteString(ltHeading.Render(name))
	b.WriteString("\n")

	if m.total > 0 {
		pct := int(float64(m.downloaded) / float64(m.total) * 100)
		sizeStr := fmt.Sprintf("%.1f / %.1f GB", float64(m.downloaded)/(1<<30), float64(m.total)/(1<<30))
		speedStr := ""
		if m.speedBps > 0 {
			speedStr = fmt.Sprintf("  %.1f MB/s", float64(m.speedBps)/(1024*1024))
		}
		fmt.Fprintf(&b, "%s %3d%%\n", m.bar.View(), pct)
		b.WriteString(ltDim.Render(sizeStr + speedStr))
	} else {
		b.WriteString(ltDim.Render(fmt.Sprintf("%.1f MB downloaded", float64(m.downloaded)/(1024*1024))))
		if m.speedBps > 0 {
			b.WriteString(ltDim.Render(fmt.Sprintf("  %.1f MB/s", float64(m.speedBps)/(1024*1024))))
		}
	}

	b.WriteString("\n\n" + ltDim.Render("ctrl+c cancel — partial download resumes next time"))
	return ltBox.Render(b.String())
}

func (m *linkPicker) doneView() string {
	var b strings.Builder
	b.WriteString(ltTitle.Render("Download complete"))
	b.WriteString("\n\n")
	b.WriteString(lpGood.Render("✓ ") + m.main.Filename + "\n")
	if m.mmproj != nil {
		b.WriteString(lpGood.Render("✓ ") + m.mmproj.Filename + "\n")
	}
	b.WriteString("\n" + ltDim.Render("Press Enter to finish"))
	return ltBox.Render(b.String())
}

func (m *linkPicker) errorView() string {
	var b strings.Builder
	b.WriteString(ltTitle.Render("Download failed"))
	b.WriteString("\n\n")
	b.WriteString(ltErr.Render(m.dlErr.Error()))
	b.WriteString("\n\n" + ltDim.Render("Press Enter to exit — partial file is kept for resume"))
	return ltBox.Render(b.String())
}

func newLinkBar() progress.Model {
	return progress.New(
		progress.WithDefaultScaledGradient(),
		progress.WithFillCharacters('█', '░'),
		progress.WithWidth(40),
		progress.WithoutPercentage(),
	)
}

func (m *linkPicker) startDownload() tea.Cmd {
	ch := make(chan tea.Msg, 100)
	m.progCh = ch
	go m.runDownload(ch)
	return linkDlListen(ch)
}

func (m *linkPicker) runDownload(ch chan<- tea.Msg) {
	defer close(ch)

	home, _ := os.UserHomeDir()
	destDir := filepath.Join(home, "models", m.slug)

	files := []download.RemoteFile{m.main}
	if m.mmproj != nil {
		files = append(files, *m.mmproj)
	}

	var total int64
	for _, f := range files {
		if f.Size > 0 {
			total += f.Size
		}
	}

	var downloaded int64
	var lastSent time.Time
	for _, f := range files {
		progCh := make(chan download.Progress, 20)
		errCh := make(chan error, 1)
		go func(f download.RemoteFile) {
			errCh <- download.Download(f, destDir, progCh)
			close(progCh)
		}(f)

		offsetBefore := downloaded
		var fileDownloaded int64
		for p := range progCh {
			fileDownloaded = p.Downloaded
			combined := offsetBefore + p.Downloaded
			now := time.Now()
			if now.Sub(lastSent) >= 100*time.Millisecond || (p.Total > 0 && p.Downloaded >= p.Total) {
				ch <- linkDlMsg{downloaded: combined, total: total, speed: p.BytesPerSec, filename: p.Filename}
				lastSent = now
			}
		}

		if err := <-errCh; err != nil {
			ch <- linkDlMsg{done: true, err: err, filename: f.Filename}
			return
		}
		downloaded += fileDownloaded
	}

	if err := validateGGUF(filepath.Join(destDir, m.main.Filename)); err != nil {
		ch <- linkDlMsg{done: true, err: err}
		return
	}

	ch <- linkDlMsg{done: true, downloaded: downloaded, total: total, filename: m.main.Filename}
}

func linkDlListen(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return linkDlMsg{done: true}
		}
		return msg
	}
}

// runHFAddTUI runs the pick/download TUI. With preselect it skips the picker
// and downloads mains[0] directly (used for /resolve/ links).
func runHFAddTUI(repo, name string, mains, mmprojs []download.RemoteFile, preselect bool) linkAddResult {
	m := linkPicker{
		repo:      repo,
		name:      name,
		mains:     mains,
		mmprojs:   mmprojs,
		preselect: preselect,
		screen:    lsPick,
		width:     80,
		height:    24,
	}

	if preselect && len(mains) > 0 {
		m.main = mains[0]
		m.mmproj = pickMmproj(mmprojs, mains[0].Filename)
		m.slug = deriveSlug(name, mains[0].Filename)
		m.bar = newLinkBar()
		m.screen = lsDownload
	}

	p := tea.NewProgram(&m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return linkAddResult{}
	}
	return final.(*linkPicker).result
}
