package download

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRejectsSplitAndTraversalBeforeRequest(t *testing.T) {
	for _, name := range []string{"../escape.gguf", "/tmp/escape.gguf", "model-00002-of-00003.gguf"} {
		err := DownloadURL("http://127.0.0.1:1/model", name, 0, t.TempDir(), nil)
		if err == nil || strings.Contains(err.Error(), "HTTP request") {
			t.Fatalf("not rejected before network: %s %v", name, err)
		}
	}
}
func TestNestedArtifactDownload(t *testing.T) {
	server := mockHFServer(t, []byte("GGUFtest"))
	defer server.Close()
	dir := t.TempDir()
	if err := DownloadURL(server.URL, "Q4/model.gguf", 8, dir, nil); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "Q4/model.gguf")); err != nil || string(data) != "GGUFtest" {
		t.Fatalf("%s %v", data, err)
	}
}
func TestResumeRejectsWrongRange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-3/8")
		w.WriteHeader(206)
		_, _ = w.Write([]byte("GGUF"))
	}))
	defer server.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "model.gguf.partial"), []byte("GGUF"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DownloadURL(server.URL, "model.gguf", 8, dir, nil); err == nil {
		t.Fatal("accepted wrong range")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "model.gguf.partial"))
	if string(b) != "GGUF" {
		t.Fatal("modified partial")
	}
}
func TestResolveRejectsMultipleShards(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, nil })
	// Use a local tree fixture through the same HTTP client as production.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"type":"file","path":"model-Q4_K_M-00001-of-00002.gguf","size":10},{"type":"file","path":"model-Q4_K_M-00002-of-00002.gguf","size":10}]`))
	}))
	defer server.Close()
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		req, _ := http.NewRequest("GET", server.URL, nil)
		return old.RoundTrip(req)
	})
	if _, err := ResolveFiles("test/repo", "Q4_K_M"); err == nil || !strings.Contains(err.Error(), "split GGUF") {
		t.Fatalf("%v", err)
	}
}
