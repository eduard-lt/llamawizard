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

	"github.com/eduard-lt/llamawizard/internal/atomicfile"
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
	existing, err := readOptional(path)
	if err != nil {
		return err
	}
	data, err := RenderModels(existing, port, "dummy", models, nil)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o600)
}

// ConfigureSettings merges pi-specific keys into ~/.pi/agent/settings.json
// without clobbering other settings (theme, etc.).
func ConfigureSettings(defaultModel string, models []state.ModelEntry) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	existing, err := readOptional(path)
	if err != nil {
		return err
	}
	data, err := RenderSettings(existing, defaultModel, models)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o600)
}

func readOptional(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return b, err
}
