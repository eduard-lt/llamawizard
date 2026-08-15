package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// linkTutorial is a small TUI that teaches the user how a --link URL should
// look, and lets them paste one in to proceed (or Esc to exit).
type linkTutorial struct {
	input     textinput.Model
	submitted string
	err       string
}

var (
	ltTitle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).MarginBottom(1)
	ltHeading = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	ltCode    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	ltDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	ltErr     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	ltBox     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
)

func newLinkTutorial() linkTutorial {
	ti := textinput.New()
	ti.Placeholder = "https://huggingface.co/<owner>/<repo>"
	ti.CharLimit = 500
	ti.Width = 80
	ti.Focus()
	return linkTutorial{input: ti}
}

func (m linkTutorial) Init() tea.Cmd {
	return textinput.Blink
}

func (m linkTutorial) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			url := strings.TrimSpace(m.input.Value())
			if url == "" {
				m.err = "Paste a link above, or press Esc to exit."
				return m, nil
			}
			m.submitted = url
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m linkTutorial) View() string {
	var b strings.Builder
	b.WriteString(ltTitle.Render("Adding a model from a link"))
	b.WriteString("\n")
	b.WriteString("You can point --link at three kinds of URL:\n\n")

	b.WriteString(ltHeading.Render("1. A direct file link (recommended)"))
	b.WriteString("\n")
	b.WriteString(ltCode.Render("   https://huggingface.co/<owner>/<repo>/resolve/main/<file>.gguf"))
	b.WriteString("\n\n")

	b.WriteString(ltHeading.Render("2. A HuggingFace repo page"))
	b.WriteString("\n")
	b.WriteString("   We'll list its .gguf files and let you pick one.")
	b.WriteString("\n")
	b.WriteString(ltCode.Render("   https://huggingface.co/<owner>/<repo>"))
	b.WriteString("\n\n")

	b.WriteString(ltHeading.Render("3. Any other direct .gguf URL"))
	b.WriteString("\n")
	b.WriteString(ltCode.Render("   https://example.com/model.gguf"))
	b.WriteString("\n\n")

	b.WriteString(ltDim.Render("Paste a link and press Enter to add it, or Esc to cancel."))
	b.WriteString("\n\n")
	b.WriteString(m.input.View())
	if m.err != "" {
		b.WriteString("\n\n" + ltErr.Render(m.err))
	}

	return ltBox.Render(b.String())
}

// runLinkTutorial launches the tutorial TUI and returns the URL the user
// pasted (or "" if they exited).
func runLinkTutorial() string {
	p := tea.NewProgram(newLinkTutorial(), tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return ""
	}
	return final.(linkTutorial).submitted
}
