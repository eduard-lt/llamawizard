package pi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eduard-lt/llamawizard/internal/state"
)

func TestConfigureModels_MergePreservesOtherProviders(t *testing.T) {
	dir := t.TempDir()
	origConfigDir := ""
	if d, err := configDir(); err == nil {
		origConfigDir = d
	}
	t.Setenv("HOME", dir)
	// Simulate existing config with an external provider.
	existing := modelsFile{
		Providers: map[string]providerConfig{
			"anthropic": {
				API:     "anthropic",
				BaseURL: "https://api.anthropic.com/v1",
				APIKey:  "sk-xxx",
				Models:  []modelEntry{{ID: "claude-sonnet", Name: "Claude Sonnet"}},
			},
		},
	}
	cfgDir, _ := configDir()
	_ = os.MkdirAll(cfgDir, 0o755)
	mp, _ := modelsPath()
	data, _ := json.MarshalIndent(existing, "", "  ")
	_ = os.WriteFile(mp, data, 0o644)

	models := []state.ModelEntry{
		{Slug: "gemma-4-26b-a4b-it", Name: "Gemma 4 26B"},
	}

	if err := ConfigureModels(8080, models); err != nil {
		t.Fatalf("ConfigureModels failed: %v", err)
	}

	got, err := os.ReadFile(mp)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}
	var result modelsFile
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	// Local provider must exist.
	local, ok := result.Providers["local"]
	if !ok {
		t.Fatal("local provider missing")
	}
	if local.BaseURL != "http://127.0.0.1:8080/v1" {
		t.Errorf("baseUrl = %q, want http://127.0.0.1:8080/v1", local.BaseURL)
	}
	if local.APIKey != "dummy" {
		t.Errorf("apiKey = %q, want dummy", local.APIKey)
	}
	if len(local.Models) != 1 || local.Models[0].ID != "gemma-4-26b-a4b-it" {
		t.Errorf("local models = %+v", local.Models)
	}

	// Anthropic must be preserved.
	if result.Providers["anthropic"].API != "anthropic" {
		t.Error("anthropic provider was clobbered")
	}

	_ = origConfigDir
}

func TestConfigureModels_UsesSlugAsNameWhenNameEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	models := []state.ModelEntry{
		{Slug: "qwen3-6-27b"},
	}
	if err := ConfigureModels(8081, models); err != nil {
		t.Fatalf("ConfigureModels failed: %v", err)
	}
	mp, _ := modelsPath()
	got, _ := os.ReadFile(mp)
	var result modelsFile
	_ = json.Unmarshal(got, &result)
	e := result.Providers["local"].Models[0]
	if e.ID != "qwen3-6-27b" {
		t.Errorf("id = %q", e.ID)
	}
	if e.Name == "" {
		t.Error("name should be derived from slug when empty")
	}
}

func TestConfigureSettings_PreservesExtraFields(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfgDir, _ := configDir()
	_ = os.MkdirAll(cfgDir, 0o755)
	sp, _ := settingsPath()

	existing := map[string]any{
		"theme":                "dark",
		"lastChangelogVersion": "0.84.1",
		"defaultProvider":      "anthropic",
		"defaultModel":         "claude-sonnet",
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	_ = os.WriteFile(sp, data, 0o644)

	models := []state.ModelEntry{
		{Slug: "gemma-4-26b-a4b-it"},
		{Slug: "qwen3-6-27b"},
	}
	if err := ConfigureSettings("qwen3-6-27b", models); err != nil {
		t.Fatalf("ConfigureSettings failed: %v", err)
	}

	got, _ := os.ReadFile(sp)
	var result2 map[string]any
	_ = json.Unmarshal(got, &result2)

	if result2["theme"] != "dark" {
		t.Error("theme was clobbered")
	}
	if result2["defaultProvider"] != "local" {
		t.Errorf("defaultProvider = %v, want local", result2["defaultProvider"])
	}
	if result2["defaultModel"] != "qwen3-6-27b" {
		t.Errorf("defaultModel = %v", result2["defaultModel"])
	}
	enabled, ok := result2["enabledModels"].([]any)
	if !ok || len(enabled) != 2 {
		t.Errorf("enabledModels = %v", result2["enabledModels"])
	}
	_ = cfgDir
}

func TestModelsPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	p, _ := modelsPath()
	expected := filepath.Join(dir, ".pi", "agent", "models.json")
	if p != expected {
		t.Errorf("modelsPath = %q, want %q", p, expected)
	}
}

func TestSettingsPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	p, _ := settingsPath()
	expected := filepath.Join(dir, ".pi", "agent", "settings.json")
	if p != expected {
		t.Errorf("settingsPath = %q, want %q", p, expected)
	}
}

func TestConfigureModels_EmptyModels(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := ConfigureModels(8080, nil); err != nil {
		t.Fatalf("ConfigureModels with nil models failed: %v", err)
	}
	mp, _ := modelsPath()
	data, _ := os.ReadFile(mp)
	var result3 modelsFile
	_ = json.Unmarshal(data, &result3)
	local := result3.Providers["local"]
	if len(local.Models) != 0 {
		t.Errorf("expected empty models, got %d", len(local.Models))
	}
}
