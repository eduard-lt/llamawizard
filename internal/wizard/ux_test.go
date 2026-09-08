package wizard

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/eduard-lt/llamawizard/internal/health"
	"github.com/eduard-lt/llamawizard/internal/state"
)

func TestPiSelectorLayoutAndScrolling(t *testing.T) {
	for _, size := range [][2]int{{60, 24}, {80, 24}, {160, 40}, {200, 40}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			m := InitialModel("test")
			m.Screen = ScreenPiDefault
			m.Width = size[0]
			m.Height = size[1]
			m.piCurrentDefault = "deepseek-v4-flash"
			for i := 0; i < 11; i++ {
				m.State.Models = append(m.State.Models, state.ModelEntry{Slug: fmt.Sprintf("model-%02d-long-quant-name-q4-k-m", i)})
			}
			check := func(selected string) {
				t.Helper()
				view := ansi.Strip(m.View())
				if lipgloss.Width(view) > m.Width {
					t.Fatalf("view too wide: %d > %d", lipgloss.Width(view), m.Width)
				}
				if lipgloss.Height(view) > m.Height {
					t.Fatalf("view too tall: %d > %d\n%s", lipgloss.Height(view), m.Height, view)
				}
				if !strings.Contains(view, selected) {
					t.Fatalf("selected row hidden: %s\n%s", selected, view)
				}
				for _, line := range strings.Split(view, "\n") {
					if strings.Contains(line, "Keep current default") && (strings.Contains(line, "deepseek") || strings.Contains(line, "model-")) {
						t.Fatal("keep option shares a line with model text")
					}
				}
			}
			check("› Keep current default")
			m.piDefaultIdx = 10
			check("› model-10-long-quant-name-q4-k-m")
			if m.Height == 24 && !strings.Contains(ansi.Strip(m.View()), "of 11") {
				t.Fatal("missing scroll position")
			}
		})
	}
}
func TestFailedHealthCannotShowSetupComplete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := InitialModel("test")
	m.Screen = ScreenHealth
	m.healthDone = true
	m.healthReport = health.Report{Error: "offline"}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.Screen != ScreenHealth || m.healthDone || cmd == nil {
		t.Fatal("failure did not trigger retry")
	}
}
func TestAddOnlyBackNavigation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := InitialAddModel("test")
	m.Screen = ScreenConfig
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.Screen != ScreenModelSelect {
		t.Fatal("add-only flow escaped into full setup")
	}
}
func TestBusyLaunchIgnoresBack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := InitialModel("test")
	m.Screen = ScreenLaunchAgent
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).Screen != ScreenLaunchAgent {
		t.Fatal("navigated during a config write")
	}
}
func TestAPIKeyPromptReflectsExistingSetting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := InitialModel("test")
	m.Width = 80
	m.apiKey = "private-existing-key"
	view := ansi.Strip(m.apiKeyView())
	if strings.Contains(view, "dummy") || strings.Contains(view, m.apiKey) || !strings.Contains(view, "keeps the current setting") {
		t.Fatalf("misleading key prompt: %s", view)
	}
}
