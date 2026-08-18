package wizard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eduard-lt/llamawizard/internal/state"
)

func TestInitialModel_PreservesExistingState(t *testing.T) {
	tmpDir := t.TempDir()

	aiDir := filepath.Join(tmpDir, ".local", "ai")
	if err := os.MkdirAll(aiDir, 0o755); err != nil {
		t.Fatal(err)
	}

	existing := &state.State{
		Port:   9000,
		APIKey: "existing-key",
		Chip:   "apple-silicon",
		Models: []state.ModelEntry{
			{Slug: "qwen3.6-27b", HFRepo: "Qwen/Qwen3.6-27B-GGUF", Quant: "Q4_K_M", SizeBytes: 15600000000, InstalledAt: "2026-08-10T00:00:00Z", Name: "qwen3.6-27b"},
		},
	}
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(aiDir, "state.json")
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tmpDir)
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })

	m := InitialModel("")

	if m.State == nil {
		t.Fatal("State is nil")
	}
	if m.State.Port != 9000 {
		t.Errorf("Port = %d, want 9000", m.State.Port)
	}
	if m.State.APIKey != "existing-key" {
		t.Errorf("APIKey = %s, want existing-key", m.State.APIKey)
	}
	if m.State.Chip != "apple-silicon" {
		t.Errorf("Chip = %s, want apple-silicon", m.State.Chip)
	}
	if len(m.State.Models) != 1 {
		t.Fatalf("len(Models) = %d, want 1", len(m.State.Models))
	}
	if m.State.Models[0].Slug != "qwen3.6-27b" {
		t.Errorf("Models[0].Slug = %s, want qwen3.6-27b", m.State.Models[0].Slug)
	}
}

func TestInitialModel_NoStateCreatesFreshDefault(t *testing.T) {
	tmpDir := t.TempDir()

	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tmpDir)
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })

	m := InitialModel("")

	if m.State == nil {
		t.Fatal("State is nil")
	}
	if m.State.Port != 8080 {
		t.Errorf("Port = %d, want 8080", m.State.Port)
	}
	if m.State.APIKey != "dummy" {
		t.Errorf("APIKey = %s, want dummy", m.State.APIKey)
	}
	if len(m.State.Models) != 0 {
		t.Errorf("len(Models) = %d, want 0", len(m.State.Models))
	}
}

func TestDownloadAppendsModels_DoesNotWipe(t *testing.T) {
	tmpDir := t.TempDir()

	aiDir := filepath.Join(tmpDir, ".local", "ai")
	if err := os.MkdirAll(aiDir, 0o755); err != nil {
		t.Fatal(err)
	}

	initial := &state.State{
		Port:   8080,
		APIKey: "dummy",
		Models: []state.ModelEntry{
			{Slug: "qwen3.6-27b", HFRepo: "Qwen/Qwen3.6-27B-GGUF", Quant: "Q4_K_M", SizeBytes: 15600000000, InstalledAt: "2026-08-10T00:00:00Z", Name: "qwen3.6-27b"},
		},
	}
	data, err := json.MarshalIndent(initial, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(aiDir, "state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tmpDir)
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })

	m := InitialModel("")

	if len(m.State.Models) != 1 {
		t.Fatalf("before append: len(Models) = %d, want 1", len(m.State.Models))
	}

	m.State.Models = append(m.State.Models, state.ModelEntry{
		Slug:        "gemma-4-26b-a4b-it",
		HFRepo:      "unsloth/gemma-4-26B-A4B-it-GGUF",
		Quant:       "Q6_K",
		SizeBytes:   21567106743,
		InstalledAt: "2026-08-11T00:00:00Z",
		Name:        "gemma-4-26b-a4b-it",
	})
	if err := m.State.Save(""); err != nil {
		t.Fatal(err)
	}

	// Re-read from disk and verify both models are present.
	loaded, err := state.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Models) != 2 {
		t.Fatalf("after save: len(Models) = %d, want 2", len(loaded.Models))
	}

	slugs := make(map[string]bool)
	for _, mod := range loaded.Models {
		slugs[mod.Slug] = true
	}
	if !slugs["qwen3.6-27b"] {
		t.Error("qwen3.6-27b missing from saved state")
	}
	if !slugs["gemma-4-26b-a4b-it"] {
		t.Error("gemma-4-26b-a4b-it missing from saved state")
	}
}

// The regression: completing a download for a slug that is already in state
// used to append a second entry and save it, so llamaswap.GenerateConfig
// rejected every later config regeneration until the duplicate was cleaned
// up. The completion handler must consult installedSlugs instead.
func TestDownloadCompletion_DuplicateSlugSkipped(t *testing.T) {
	tmpDir := t.TempDir()

	aiDir := filepath.Join(tmpDir, ".local", "ai")
	if err := os.MkdirAll(aiDir, 0o755); err != nil {
		t.Fatal(err)
	}

	initial := &state.State{
		Port:   8080,
		APIKey: "dummy",
		Models: []state.ModelEntry{
			{Slug: "qwen3.6-27b", HFRepo: "Qwen/Qwen3.6-27B-GGUF", Quant: "Q4_K_M", SizeBytes: 15600000000, InstalledAt: "2026-08-10T00:00:00Z", Name: "qwen3.6-27b"},
		},
	}
	data, err := json.MarshalIndent(initial, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(aiDir, "state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tmpDir)
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })

	m := InitialModel("")
	if len(m.State.Models) != 1 {
		t.Fatalf("before: len(Models) = %d, want 1", len(m.State.Models))
	}

	// Re-downloading an already-installed model must not duplicate it.
	next, _ := m.Update(dlProgressMsg{modelIdx: -1, slug: "qwen3.6-27b", filename: "f.gguf", repo: "Qwen/Qwen3.6-27B-GGUF", quant: "Q4_K_M", size: 15600000000, done: true})
	m = next.(Model)
	if len(m.State.Models) != 1 {
		t.Fatalf("after duplicate completion: len(Models) = %d, want 1", len(m.State.Models))
	}

	// A genuinely new slug is still appended and remembered for the session.
	next, _ = m.Update(dlProgressMsg{modelIdx: -1, slug: "gemma-4-26b-a4b-it", filename: "g.gguf", repo: "unsloth/gemma-4-26B-A4B-it-GGUF", quant: "Q6_K", size: 21567106743, done: true})
	m = next.(Model)
	if len(m.State.Models) != 2 {
		t.Fatalf("after new completion: len(Models) = %d, want 2", len(m.State.Models))
	}
	if !m.installedSlugs["gemma-4-26b-a4b-it"] {
		t.Error("new slug not recorded in installedSlugs")
	}

	// And the saved state on disk must be duplicate-free.
	loaded, err := state.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Models) != 2 {
		t.Fatalf("saved state: len(Models) = %d, want 2", len(loaded.Models))
	}
	for i, a := range loaded.Models {
		for j := i + 1; j < len(loaded.Models); j++ {
			if a.Slug == loaded.Models[j].Slug {
				t.Fatalf("duplicate slug %q in saved state", a.Slug)
			}
		}
	}
}
