package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const currentSchemaVersion = 1

// State is the source of truth for llamawizard. Every package reads/writes
// through this struct so that re-runs are idempotent.
type State struct {
	SchemaVersion   int          `json:"schema_version"`
	Port            int          `json:"port"`
	APIKey          string       `json:"api_key"`
	Chip            string       `json:"chip"`
	LlamaCppPath    string       `json:"llama_cpp_path"`
	LlamaSwapPath   string       `json:"llama_swap_path"`
	Models          []ModelEntry `json:"models"`
	LastHealthCheck *time.Time   `json:"last_health_check,omitempty"`
	PiConfigured    bool         `json:"pi_configured"`
}

// ModelEntry describes a single installed model.
type ModelEntry struct {
	Slug        string `json:"slug"`
	HFRepo      string `json:"hf_repo"`
	Quant       string `json:"quant"`
	File        string `json:"file"`
	Mmproj      string `json:"mmproj,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
	InstalledAt string `json:"installed_at"`

	// Optional display / config fields populated by the wizard or user.
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	Aliases       []string `json:"aliases,omitempty"`
	CtxSize       int      `json:"ctx_size,omitempty"`       // 0 = default
	Temperature   float64  `json:"temperature,omitempty"`    // 0 = omit from cmd
	TopK          int      `json:"top_k,omitempty"`          // 0 = omit from cmd
	TopP          float64  `json:"top_p,omitempty"`          // 0 = omit from cmd
	RepeatPenalty float64  `json:"repeat_penalty,omitempty"` // 0 = omit from cmd
}

// DeriveSlug produces a clean model slug from an artifact filename.
//
// It strips the .gguf extension and removes the prefix before the first
// hyphen (the repo/author tag), leaving the model version + quant type.
//
// Examples:
//
//	"Qwen-Qwen3.6-27B-Q4_K_M.gguf" → "Qwen3.6-27B-Q4_K_M"
//	"gemma-4-26B-A4B-it-Q4_K_M.gguf" → "4-26B-A4B-it-Q4_K_M"
//	"model.gguf" → "model"
func DeriveSlug(filename string) string {
	name := filename

	// Strip .gguf extension (case-insensitive).
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".gguf") {
		name = name[:len(name)-5]

		// Strip prefix before first hyphen (e.g. "Qwen-" or "gemma-").
		if idx := strings.Index(name, "-"); idx > 0 {
			name = name[idx+1:]
		}
	}

	return name
}

// DeriveSlugFromModelID produces a clean model slug from a HuggingFace
// model ID (e.g. "ggml-org/gemma-4-26B-A4B-it-GGUF" → "gemma-4-26b-a4b-it-gguf").
func DeriveSlugFromModelID(modelID string) string {
	parts := strings.Split(modelID, "/")
	name := parts[len(parts)-1]
	name = strings.ReplaceAll(name, "_", "-")
	return strings.ToLower(name)
}

// DefaultPath returns ~/.local/ai/state.json.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/llamawizard-state.json"
	}
	return filepath.Join(home, ".local", "ai", "state.json")
}

// DefaultConfigPath returns ~/.local/ai/config/llama-swap.yaml.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/llama-swap.yaml"
	}
	return filepath.Join(home, ".local", "ai", "config", "llama-swap.yaml")
}

// Load reads the state file. If it doesn't exist (first run), returns a
// zero-value State — not an error. The caller can detect first-run by
// checking if Port == 0 or Models is nil.
func Load(path string) (*State, error) {
	if path == "" {
		path = DefaultPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{}, nil // first run
		}
		return nil, err
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Save writes the state to disk, creating parent directories as needed.
func (s *State) Save(path string) error {
	if path == "" {
		path = DefaultPath()
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	s.SchemaVersion = currentSchemaVersion

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}
