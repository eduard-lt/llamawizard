package health

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Report is the result of a health check against the llama-swap API.
type Report struct {
	Pass          bool     `json:"pass"`
	Port          int      `json:"port"`
	FoundModels   []string `json:"found_models"`
	MissingModels []string `json:"missing_models,omitempty"`
	Error         string   `json:"error,omitempty"`
	ErrorLogTail  string   `json:"error_log_tail,omitempty"`
	ErrorLogPath  string   `json:"error_log_path,omitempty"`
	Attempts      int      `json:"attempts"`
	Duration      string   `json:"duration"`
}

type v1ModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// Check polls the llama-swap /v1/models endpoint with exponential backoff
// and verifies that every expected model id is present.
//
// The first attempt is immediate; retryable failures are retried after
// 2s, 4s, 8s, 16s (max 5 attempts, ~30s total wait).
// On failure the report includes the last 20 lines of the error log at
// ~/.local/ai/logs/llama-swap-error.log.
func Check(port int, expectedModels []string) (Report, error) {
	return CheckWithKey(port, expectedModels, "")
}

// CheckWithKey is like Check but includes an API key in the Authorization header.
func CheckWithKey(port int, expectedModels []string, apiKey string) (Report, error) {
	start := time.Now()

	// Wait before each attempt; 0 means the first attempt is immediate.
	waits := []time.Duration{
		0,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/v1/models", port)

	var lastErr error
	var resp *v1ModelsResponse
	attempt := 0

	for i, wait := range waits {
		attempt = i + 1

		if wait > 0 {
			time.Sleep(wait)
		}

		resp, lastErr = fetchModels(url, apiKey)
		if lastErr == nil || !isRetryable(lastErr) {
			break
		}
	}

	r := Report{
		Port:     port,
		Attempts: attempt,
		Duration: time.Since(start).Round(time.Millisecond).String(),
	}

	if lastErr != nil {
		r.Error = lastErr.Error()
		r.enrichErrorLog()
		return r, nil
	}

	for _, m := range resp.Data {
		r.FoundModels = append(r.FoundModels, m.ID)
	}

	expected := make(map[string]bool)
	for _, id := range expectedModels {
		expected[id] = true
	}

	found := make(map[string]bool)
	for _, id := range r.FoundModels {
		found[id] = true
	}

	for _, id := range expectedModels {
		if !found[id] {
			r.MissingModels = append(r.MissingModels, id)
		}
	}

	r.Pass = len(r.MissingModels) == 0 && len(expectedModels) > 0

	if !r.Pass && len(expectedModels) > 0 {
		r.enrichErrorLog()
	}

	return r, nil
}

func fetchModels(url, apiKey string) (*v1ModelsResponse, error) {
	client := &http.Client{Timeout: 3 * time.Second}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, &retryableError{err}
	}

	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, &retryableError{err}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, &httpError{resp.StatusCode, strings.TrimSpace(string(body))}
	}

	var data v1ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, &httpError{resp.StatusCode, fmt.Sprintf("json decode: %v", err)}
	}

	return &data, nil
}

type retryableError struct{ error }

func (e *retryableError) Unwrap() error { return e.error }

type httpError struct {
	code int
	body string
}

func (e *httpError) Error() string {
	if e.body != "" {
		return fmt.Sprintf("HTTP %d: %s", e.code, e.body)
	}
	return fmt.Sprintf("HTTP %d", e.code)
}

func isRetryable(err error) bool {
	_, ok := err.(*retryableError)
	return ok
}

func errorLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "ai", "logs", "llama-swap-error.log"), nil
}

func (r *Report) enrichErrorLog() {
	path, err := errorLogPath()
	if err != nil {
		return
	}

	r.ErrorLogPath = path

	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	n := len(lines)
	start := 0
	if n > 20 {
		start = n - 20
	}

	r.ErrorLogTail = strings.Join(lines[start:], "\n")
}
