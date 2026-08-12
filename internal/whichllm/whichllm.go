package whichllm

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

// ModelCandidate is one row from whichllm's --json output.
type ModelCandidate struct {
	Rank                       int       `json:"rank"`
	ModelID                    string    `json:"model_id"`
	ArtifactRepoID             *string   `json:"artifact_repo_id"`
	ArtifactFilename           *string   `json:"artifact_filename"`
	ParameterCount             int64     `json:"parameter_count"`
	PublishedAt                string    `json:"published_at"`
	Downloads                  int64     `json:"downloads"`
	QuantType                  string    `json:"quant_type"`
	FileSizeBytes              int64     `json:"file_size_bytes"`
	VRAMRequiredBytes          int64     `json:"vram_required_bytes"`
	VRAMAvailableBytes         int64     `json:"vram_available_bytes"`
	UsesMultiGPU               bool      `json:"uses_multi_gpu"`
	MultiGPUEffectiveVRAMBytes *int64    `json:"multi_gpu_effective_vram_bytes"`
	EstimatedTokPerSec         float64   `json:"estimated_tok_per_sec"`
	SpeedConfidence            string    `json:"speed_confidence"`
	SpeedRangeTokPerSec        []float64 `json:"speed_range_tok_per_sec"`
	SpeedNotes                 []string  `json:"speed_notes"`
	QualityScore               float64   `json:"quality_score"`
	BenchmarkStatus            string    `json:"benchmark_status"`
	BenchmarkSource            string    `json:"benchmark_source"`
	BenchmarkConfidence        float64   `json:"benchmark_confidence"`
	FitType                    string    `json:"fit_type"`
	CanRun                     bool      `json:"can_run"`
	Warnings                   []string  `json:"warnings"`
	License                    string    `json:"license"`
}

// whichllmResponse is the top-level JSON shape from `whichllm --json`.
type whichllmResponse struct {
	Hardware map[string]json.RawMessage `json:"hardware"`
	Models   []ModelCandidate           `json:"models"`
}

// IsAvailable checks whether `uv` is on PATH (required for uvx whichllm).
func IsAvailable() bool {
	_, err := exec.LookPath("uv")
	return err == nil
}

// Rank shells out to `uvx whichllm@latest --profile <profile> --top <n> --json`
// and returns the ranked model list.
func Rank(profile string, top int) ([]ModelCandidate, error) {
	if !IsAvailable() {
		return nil, fmt.Errorf("uv not found on PATH — install with: brew install uv")
	}

	cmd := exec.Command("uvx", "whichllm@latest",
		"--profile", profile,
		"--top", fmt.Sprint(top),
		"--json",
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("running whichllm: %w", err)
	}

	var resp whichllmResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parsing whichllm JSON: %w", err)
	}

	return resp.Models, nil
}
