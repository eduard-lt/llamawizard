package whichllm

import (
	"encoding/json"
	"os"
	"testing"
)

func TestIsAvailable(t *testing.T) {
	if !IsAvailable() {
		t.Error("expected uv to be available on PATH")
	}
}

// TestParseFixture feeds the saved whichllm JSON fixture through the parser
// and verifies struct fields match the real output.
func TestParseFixture(t *testing.T) {
	data, err := os.ReadFile("fixture.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	var resp whichllmResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	if len(resp.Models) != 3 {
		t.Fatalf("expected 3 models in fixture, got %d", len(resp.Models))
	}

	// Rank 1 — Qwen3.6-27B
	m0 := resp.Models[0]
	assertField(t, "rank", m0.Rank, 1)
	assertField(t, "model_id", m0.ModelID, "Qwen/Qwen3.6-27B")
	assertField(t, "quant_type", m0.QuantType, "Q4_K_M")
	assertField(t, "fit_type", m0.FitType, "full_gpu")
	assertField(t, "can_run", m0.CanRun, true)
	assertField(t, "benchmark_confidence", m0.BenchmarkConfidence, 1.0)
	assertField(t, "quality_score", m0.QualityScore, 87.31)

	// artifact_repo_id is non-null for this model.
	if m0.ArtifactRepoID == nil {
		t.Error("expected ArtifactRepoID to be set for rank 1")
	} else if *m0.ArtifactRepoID != "batiai/Qwen3.6-27B-GGUF" {
		t.Errorf("ArtifactRepoID: want batiai/Qwen3.6-27B-GGUF, got %s", *m0.ArtifactRepoID)
	}

	// Rank 2 — gemma-4-26B (artifact_repo_id is null for this one)
	m1 := resp.Models[1]
	assertField(t, "model_id", m1.ModelID, "google/gemma-4-26B-A4B-it")
	assertField(t, "quant_type", m1.QuantType, "Q6_K")
	if m1.ArtifactRepoID != nil {
		t.Errorf("expected ArtifactRepoID to be nil for gemma-4, got %s", *m1.ArtifactRepoID)
	}

	// Rank 3 — Qwen3-30B-A3B
	m2 := resp.Models[2]
	assertField(t, "model_id", m2.ModelID, "Qwen/Qwen3-30B-A3B")
	assertField(t, "quant_type", m2.QuantType, "Q8_0")
	assertField(t, "license", m2.License, "apache-2.0")

	// Verify numeric fields parse correctly (not zero).
	if m0.ParameterCount == 0 {
		t.Error("ParameterCount should not be zero")
	}
	if m0.FileSizeBytes == 0 {
		t.Error("FileSizeBytes should not be zero")
	}
	if m0.EstimatedTokPerSec == 0 {
		t.Error("EstimatedTokPerSec should not be zero")
	}
	if len(m0.SpeedRangeTokPerSec) != 2 {
		t.Errorf("SpeedRangeTokPerSec: want 2 entries, got %d", len(m0.SpeedRangeTokPerSec))
	}
}

func assertField[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: want %#v, got %#v", name, want, got)
	}
}
