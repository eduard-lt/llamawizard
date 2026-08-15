package pi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/eduard-lt/llamawizard/internal/state"
)

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".pi", "agent"), nil
}

func modelsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "models.json"), nil
}

func settingsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

func IsInstalled() bool {
	_, err := exec.LookPath("pi")
	return err == nil
}

// CurrentDefaultModel returns pi's current default model ID, or "" if it is
// not configured or cannot be read.
func CurrentDefaultModel() string {
	path, err := settingsPath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var sf settingsFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return ""
	}
	return sf.DefaultModel
}

func Install() error {
	cmd := exec.Command("npm", "install", "-g", "@earendil-works/pi-coding-agent")
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pi install failed:\n%s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

func Uninstall() error {
	cmd := exec.Command("npm", "uninstall", "-g", "@earendil-works/pi-coding-agent")
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pi uninstall failed:\n%s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

type modelsFile struct {
	Providers map[string]providerConfig `json:"providers"`
}

type providerConfig struct {
	API     string       `json:"api"`
	BaseURL string       `json:"baseUrl"`
	APIKey  string       `json:"apiKey"`
	Models  []modelEntry `json:"models"`
}

type modelEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type settingsFile struct {
	DefaultProvider string   `json:"defaultProvider,omitempty"`
	DefaultModel    string   `json:"defaultModel,omitempty"`
	EnabledModels   []string `json:"enabledModels,omitempty"`
	raw             map[string]any
}

func (s *settingsFile) UnmarshalJSON(data []byte) error {
	s.raw = make(map[string]any)
	if err := json.Unmarshal(data, &s.raw); err != nil {
		return err
	}
	type alias settingsFile
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	s.DefaultProvider = a.DefaultProvider
	s.DefaultModel = a.DefaultModel
	s.EnabledModels = a.EnabledModels
	return nil
}

func (s settingsFile) MarshalJSON() ([]byte, error) {
	if s.raw == nil {
		s.raw = make(map[string]any)
	}
	s.raw["defaultProvider"] = s.DefaultProvider
	s.raw["defaultModel"] = s.DefaultModel
	s.raw["enabledModels"] = s.EnabledModels
	return json.Marshal(s.raw)
}

// ConfigureModels merges a "local" provider into ~/.pi/agent/models.json
// without clobbering other providers. The local provider is keyed to
// the llama-swap endpoint at the given port.
func ConfigureModels(port int, models []state.ModelEntry) error {
	path, err := modelsPath()
	if err != nil {
		return err
	}

	mf := modelsFile{Providers: make(map[string]providerConfig)}

	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &mf); err != nil {
			return fmt.Errorf("parsing existing models.json: %w", err)
		}
	}

	var entries []modelEntry
	for _, m := range models {
		id := m.Slug
		name := m.Name
		if name == "" {
			name = strings.ReplaceAll(id, "-", " ")
			var runes []rune
			cap := true
			for _, r := range name {
				if r == ' ' {
					cap = true
					runes = append(runes, r)
				} else if cap {
					runes = append(runes, []rune(strings.ToUpper(string(r)))...)
					cap = false
				} else {
					runes = append(runes, r)
				}
			}
			name = string(runes)
		}
		entries = append(entries, modelEntry{ID: id, Name: name})
	}

	mf.Providers["local"] = providerConfig{
		API:     "openai-completions",
		BaseURL: fmt.Sprintf("http://127.0.0.1:%d/v1", port),
		APIKey:  "dummy",
		Models:  entries,
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating pi config directory: %w", err)
	}

	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling models.json: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing models.json: %w", err)
	}

	return nil
}

// ConfigureSettings merges pi-specific keys into ~/.pi/agent/settings.json
// without clobbering other settings (theme, etc.).
func ConfigureSettings(defaultModel string, models []state.ModelEntry) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}

	var sf settingsFile
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &sf); err != nil {
			return fmt.Errorf("parsing existing settings.json: %w", err)
		}
	}

	sf.DefaultProvider = "local"
	sf.DefaultModel = defaultModel

	var enabled []string
	for _, m := range models {
		enabled = append(enabled, m.Slug)
	}
	sf.EnabledModels = enabled

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating pi config directory: %w", err)
	}

	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling settings.json: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing settings.json: %w", err)
	}

	return nil
}
