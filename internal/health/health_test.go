package health

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheck_AllModelsPresent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		resp := v1ModelsResponse{
			Data: []struct {
				ID string `json:"id"`
			}{
				{ID: "model-a"},
				{ID: "model-b"},
				{ID: "model-c"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	port := ts.Listener.Addr().(*net.TCPAddr).Port

	r, err := Check(port, []string{"model-a", "model-b"})
	if err != nil {
		t.Fatal(err)
	}

	if !r.Pass {
		t.Errorf("expected Pass=true, got false. Missing: %v", r.MissingModels)
	}

	if len(r.FoundModels) != 3 {
		t.Errorf("expected 3 found models, got %d", len(r.FoundModels))
	}

	if len(r.MissingModels) != 0 {
		t.Errorf("expected 0 missing models, got %v", r.MissingModels)
	}
}

func TestCheck_MissingModels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := v1ModelsResponse{
			Data: []struct {
				ID string `json:"id"`
			}{
				{ID: "model-a"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	port := ts.Listener.Addr().(*net.TCPAddr).Port

	r, err := Check(port, []string{"model-a", "model-b"})
	if err != nil {
		t.Fatal(err)
	}

	if r.Pass {
		t.Error("expected Pass=false when model-b is missing")
	}

	if len(r.MissingModels) != 1 {
		t.Errorf("expected 1 missing model, got %d: %v", len(r.MissingModels), r.MissingModels)
	}

	if r.MissingModels[0] != "model-b" {
		t.Errorf("expected missing 'model-b', got %q", r.MissingModels[0])
	}
}

func TestCheck_EmptyExpected(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := v1ModelsResponse{
			Data: []struct {
				ID string `json:"id"`
			}{
				{ID: "model-a"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	port := ts.Listener.Addr().(*net.TCPAddr).Port

	r, err := Check(port, []string{})
	if err != nil {
		t.Fatal(err)
	}

	if r.Pass {
		t.Error("expected Pass=false when expectedModels is empty (nothing to verify)")
	}
}

func TestCheck_NoModelsAtAll(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := v1ModelsResponse{Data: nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	port := ts.Listener.Addr().(*net.TCPAddr).Port

	r, err := Check(port, []string{"model-a"})
	if err != nil {
		t.Fatal(err)
	}

	if r.Pass {
		t.Error("expected Pass=false when no models are loaded")
	}

	if len(r.FoundModels) != 0 {
		t.Errorf("expected 0 found models, got %d", len(r.FoundModels))
	}

	if len(r.MissingModels) != 1 {
		t.Errorf("expected 1 missing model, got %d", len(r.MissingModels))
	}
}

func TestCheck_WrongPort(t *testing.T) {
	r, err := Check(19999, []string{"model-a"})
	if err != nil {
		t.Fatal(err)
	}

	if r.Pass {
		t.Error("expected Pass=false for unreachable port")
	}

	if r.Error == "" {
		t.Error("expected error message for unreachable port")
	}

	if r.Attempts < 1 {
		t.Error("expected at least 1 attempt")
	}
}

func TestCheck_WrongEndpointReturns500(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	port := ts.Listener.Addr().(*net.TCPAddr).Port

	r, err := Check(port, []string{"model-a"})
	if err != nil {
		t.Fatal(err)
	}

	if r.Pass {
		t.Error("expected Pass=false for 500 response")
	}

	if r.Error == "" {
		t.Error("expected error message for 500 response")
	}
}

func TestCheck_BadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	defer ts.Close()

	port := ts.Listener.Addr().(*net.TCPAddr).Port

	r, err := Check(port, []string{"model-a"})
	if err != nil {
		t.Fatal(err)
	}

	if r.Pass {
		t.Error("expected Pass=false for bad JSON response")
	}
}

func TestEnrichErrorLog_ReadsLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, ".local", "ai", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(logDir, "llama-swap-error.log")
	var logLines []string
	for i := 0; i < 25; i++ {
		logLines = append(logLines, fmt.Sprintf("line %02d: some error message", i))
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(logLines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tmpDir)
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })

	r := &Report{
		Pass: false,
		Port: 8080,
	}
	r.enrichErrorLog()

	if r.ErrorLogPath == "" {
		t.Error("ErrorLogPath should not be empty")
	}

	if !strings.HasSuffix(r.ErrorLogPath, "llama-swap-error.log") {
		t.Errorf("ErrorLogPath should end with llama-swap-error.log, got %q", r.ErrorLogPath)
	}

	if r.ErrorLogTail == "" {
		t.Error("ErrorLogTail should not be empty")
	}

	tailLines := strings.Split(r.ErrorLogTail, "\n")
	if len(tailLines) != 20 {
		t.Errorf("expected 20 tail lines, got %d", len(tailLines))
	}

	if !strings.Contains(tailLines[len(tailLines)-1], "line 24") {
		t.Errorf("last tail line should be line 24, got %q", tailLines[len(tailLines)-1])
	}
}

func TestEnrichErrorLog_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()

	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tmpDir)
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })

	r := &Report{Pass: false, Port: 8080}
	r.enrichErrorLog()

	if r.ErrorLogPath == "" {
		t.Error("ErrorLogPath should be set even when file is missing")
	}

	if r.ErrorLogTail != "" {
		t.Error("ErrorLogTail should be empty when log file is missing")
	}
}

func TestEnrichErrorLog_Under20Lines(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, ".local", "ai", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(logDir, "llama-swap-error.log")
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tmpDir)
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })

	r := &Report{Pass: false, Port: 8080}
	r.enrichErrorLog()

	tailLines := strings.Split(r.ErrorLogTail, "\n")
	if len(tailLines) != 3 {
		t.Errorf("expected 3 tail lines for a 3-line file, got %d", len(tailLines))
	}
}

func TestReport_JSONRoundTrip(t *testing.T) {
	r := Report{
		Pass:          false,
		Port:          8080,
		FoundModels:   []string{"a"},
		MissingModels: []string{"b"},
		Error:         "connection refused",
		ErrorLogTail:  "log line 1\nlog line 2",
		ErrorLogPath:  "/tmp/test.log",
		Attempts:      3,
		Duration:      "5s",
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}

	var r2 Report
	if err := json.Unmarshal(data, &r2); err != nil {
		t.Fatal(err)
	}

	if r2.Pass != r.Pass || r2.Port != r.Port || r2.Error != r.Error {
		t.Error("JSON round-trip mismatch")
	}
}
