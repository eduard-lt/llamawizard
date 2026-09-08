package wizard

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eduard-lt/llamawizard/internal/build"
	"github.com/eduard-lt/llamawizard/internal/hardware"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eduard-lt/llamawizard/internal/llamaswap"
	"github.com/eduard-lt/llamawizard/internal/state"
)

func TestFinalConfigurationPersistsChosenPortKeyAndPaths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := InitialModel("test")
	m.port = 9123
	m.apiKey = "custom-secret"
	m.llamaCppPath = "/custom/llama-server"
	m.llamaSwapPath = "/custom/llama-swap"
	m.State.Models = []state.ModelEntry{{Slug: "base", File: "model.gguf", CtxSize: 16384}}
	m.piOptIn = true
	m.piDefaultSlug = "base"
	if err := saveConfiguration(m); err != nil {
		t.Fatal(err)
	}
	saved, err := state.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Port != 9123 || saved.APIKey != m.apiKey || saved.LlamaCppPath != m.llamaCppPath || saved.LlamaSwapPath != m.llamaSwapPath {
		t.Fatalf("bad persisted state: %+v", saved)
	}
	data, err := os.ReadFile(state.DefaultConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := llamaswap.Catalog(data)
	if err != nil || key != m.apiKey {
		t.Fatalf("key %q %v", key, err)
	}
	home, _ := os.UserHomeDir()
	if _, err = os.Stat(filepath.Join(home, ".pi/agent/models.json")); err != nil {
		t.Fatal(err)
	}
}
func TestConfigPreviewDoesNotWriteAndErrorBlocksAdvance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := InitialModel("test")
	m.Screen = ScreenConfig
	m.State.Models = []state.ModelEntry{{Slug: "base", File: "model.gguf", CtxSize: 8192}}
	m.llamaCppPath = "/server"
	msg := runGenerateConfig(m)()
	updated, _ := m.Update(msg)
	m = updated.(Model)
	if m.configErr != nil {
		t.Fatal(m.configErr)
	}
	if _, err := os.Stat(state.DefaultConfigPath()); !os.IsNotExist(err) {
		t.Fatal("preview wrote master")
	}
	m.configErr = os.ErrPermission
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.(Model).Screen != ScreenConfig {
		t.Fatal("advanced after config error")
	}
}
func TestAddOnlySkipsDefaultSelectionAndPreservesMasterKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := InitialModel("test")
	m.addOnly = true
	m.piOptIn = true
	m.State.PiConfigured = true
	m.State.Models = []state.ModelEntry{{Slug: "base", File: "model.gguf", CtxSize: 8192}}
	m.llamaCppPath = "/server"
	m.apiKey = "stale"
	if err := os.MkdirAll(filepath.Dir(state.DefaultConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	master := []byte("apiKeys: [edited-key]\nmodels:\n  base:\n    cmd: /server --ctx-size 65536\n")
	if err := os.WriteFile(state.DefaultConfigPath(), master, 0o600); err != nil {
		t.Fatal(err)
	}
	m.Screen = ScreenConfig
	m.configYAML = master
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.(Model).Screen != ScreenHealth {
		t.Fatal("add unexpectedly asks to reset Pi default")
	}
	if err := saveConfiguration(m); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(state.DefaultConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := llamaswap.Catalog(data)
	if err != nil || key != "edited-key" {
		t.Fatalf("overwrote master credentials: %q %v", key, err)
	}
}

// Exercise the normal no-argument wizard flow, not only the config helpers.
// External dependency/build results are supplied as messages; the real config
// generation, key handling and final persistence commands execute on a temp HOME.
func TestFullSetupEnterThroughPreservesCustomConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fakeBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeBin, "pi"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "launchctl"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+":"+os.Getenv("PATH"))
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer custom-key" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"base"},{"id":"corp-ceo"}]}`))
	}))
	defer api.Close()
	st := &state.State{Port: api.Listener.Addr().(*net.TCPAddr).Port, APIKey: "custom-key", PiConfigured: true, Models: []state.ModelEntry{{Slug: "base", File: "base.gguf"}}}
	if err := st.Save(""); err != nil {
		t.Fatal(err)
	}
	master := []byte("apiKeys: [custom-key]\nmodels:\n  base:\n    cmd: /server --ctx-size 98304 --cache-type-k q8_0\n    aliases: [coder]\n  corp-ceo:\n    cmd: /server --ctx-size 65536\n")
	if err := os.MkdirAll(filepath.Dir(state.DefaultConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.DefaultConfigPath(), master, 0o600); err != nil {
		t.Fatal(err)
	}
	piDir := filepath.Join(home, ".pi/agent")
	if err := os.MkdirAll(piDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := []byte(`{"defaultProvider":"deepseek","defaultModel":"deepseek-v4-flash","enabledModels":["deepseek/*","local/corp-*"],"theme":"dark"}`)
	if err := os.WriteFile(filepath.Join(piDir, "settings.json"), settings, 0o600); err != nil {
		t.Fatal(err)
	}
	models := []byte(`{"providers":{"external":{"headers":{"x-custom":"keep"}},"local":{"models":[{"id":"base","maxTokens":1024,"reasoning":true}]}}}`)
	if err := os.WriteFile(filepath.Join(piDir, "models.json"), models, 0o600); err != nil {
		t.Fatal(err)
	}
	m := InitialModel("test")
	send := func(msg tea.Msg) tea.Cmd { updated, cmd := m.Update(msg); m = updated.(Model); return cmd }
	enter := func(screen Screen) tea.Cmd {
		t.Helper()
		if m.Screen != screen {
			t.Fatalf("screen=%v want %v", m.Screen, screen)
		}
		return send(tea.KeyMsg{Type: tea.KeyEnter})
	}
	enter(ScreenWelcome)
	send(depsCheckedMsg{statuses: []build.DepStatus{{Name: "Homebrew", Present: true}, {Name: "Xcode CLT", Present: true}}})
	enter(ScreenDeps)
	send(hwDetectedMsg{info: hardware.HardwareInfo{Chip: "apple-silicon"}})
	enter(ScreenHardware)
	send(modelsLoadedMsg{})
	enter(ScreenModelSelect)
	send(buildDoneMsg{llamaCppPath: "/server"})
	send(buildDoneMsg{llamaSwapPath: "/swap"})
	cmd := enter(ScreenBuild)
	send(cmd())
	enter(ScreenConfig)
	send(piSetupDoneMsg{})
	if m.piDefaultIdx != -1 || !strings.Contains(m.piDefaultView(), "deepseek-v4-flash") {
		t.Fatal("existing default is not the preselected keep option")
	}
	enter(ScreenPiDefault)
	if m.piDefaultSlug != "" {
		t.Fatal("Enter requested a new Pi default")
	}
	enter(ScreenPort)
	send(portCheckMsg{free: true})
	enter(ScreenPort)
	cmd = enter(ScreenAPIKey)
	send(cmd())
	if m.launchErr != nil {
		t.Fatal(m.launchErr)
	}
	cmd = enter(ScreenLaunchAgent)
	send(cmd())
	if !m.healthReport.Pass {
		t.Fatalf("health failed: %+v", m.healthReport)
	}
	enter(ScreenHealth)
	if m.Screen != ScreenDone {
		t.Fatal("wizard did not complete")
	}
	current, err := os.ReadFile(state.DefaultConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	beforeModels, _, _ := llamaswap.Catalog(master)
	afterModels, _, err := llamaswap.Catalog(current)
	if err != nil || !reflect.DeepEqual(beforeModels, afterModels) {
		t.Fatalf("master changed: %s %v", current, err)
	}
	b, err := os.ReadFile(filepath.Join(piDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]any
	if err := json.Unmarshal(settings, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("Pi settings reset: %s", b)
	}
	b, err = os.ReadFile(filepath.Join(piDir, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"reasoning": true`) || !strings.Contains(string(b), `"maxTokens": 1024`) || !strings.Contains(string(b), `"external"`) {
		t.Fatalf("Pi options lost: %s", b)
	}
}

func TestPiDefaultChangeRequiresExplicitNavigation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := InitialModel("test")
	m.Screen = ScreenPiDefault
	m.piSetupDone = true
	m.configYAML = []byte("models:\n  custom-profile:\n    name: My profile\n    cmd: /server\n")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.piDefaultSlug != "custom-profile" {
		t.Fatalf("explicit profile selection ignored: %q", m.piDefaultSlug)
	}
}
