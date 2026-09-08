package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eduard-lt/llamawizard/internal/state"
)

// Runs the real compiled CLI, with isolated files, fake launchctl and a local
// authenticated API. No installed models, packages or LaunchAgents are touched.
func TestCLIConfigLifecycle(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(t.TempDir(), "llamawizard")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	t.Setenv("HOME", home)
	tools := t.TempDir()
	record := filepath.Join(home, "launchctl.calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$LAUNCH_TEST_RECORD\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(tools, "launchctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/new.gguf" {
			_, _ = w.Write([]byte("GGUFtest-new"))
			return
		}
		if r.Header.Get("Authorization") != "Bearer custom-key" {
			w.WriteHeader(401)
			return
		}
		// Keeping base in the response after removal detects only required models;
		// persona must stay available because it shares the file.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"base"},{"id":"persona"},{"id":"new"}]}`))
	}))
	defer server.Close()
	st := &state.State{Port: server.Listener.Addr().(*net.TCPAddr).Port, APIKey: "custom-key", LlamaCppPath: "/server", PiConfigured: true, Models: []state.ModelEntry{{Slug: "base", File: "model.gguf", CtxSize: 8192}}}
	if err := st.Save(""); err != nil {
		t.Fatal(err)
	}
	modelDir := filepath.Join(home, "models/base")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "model.gguf"), []byte("GGUFtest"), 0o600); err != nil {
		t.Fatal(err)
	}
	master := "apiKeys: [custom-key]\nmodels:\n  base:\n    cmd: /server --model '" + filepath.Join(modelDir, "model.gguf") + "' --ctx-size 98304\n    aliases: [fast]\n  persona:\n    cmd: /server --model '" + filepath.Join(modelDir, "model.gguf") + "' --ctx-size 65536\n"
	if err := os.MkdirAll(filepath.Dir(state.DefaultConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.DefaultConfigPath(), []byte(master), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "PATH="+tools+":"+os.Getenv("PATH"), "LAUNCH_TEST_RECORD="+record)
		cmd.Stdin = strings.NewReader("y\n")
		return cmd.CombinedOutput()
	}
	// Command-specific help, including destructive commands, must stay read-only.
	for _, args := range [][]string{{"-help"}, {"start", "--help"}, {"models", "delete", "base", "--help"}, {"config", "apply", "--help"}} {
		if out, err := run(args...); err != nil {
			t.Fatalf("help %v: %v %s", args, err, out)
		}
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatal("help invoked launchctl")
	}
	// Version/help are read-only; the old automatic migration could rewrite files.
	if out, err := run("help"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if b, _ := os.ReadFile(state.DefaultConfigPath()); string(b) != master {
		t.Fatal("help changed master")
	}
	if out, err := run("config", "apply", "--dry-run"); err != nil || !bytes.Contains(out, []byte("Preview only")) {
		t.Fatalf("%v %s", err, out)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatal("dry run restarted service")
	}
	if _, err := os.Stat(filepath.Join(home, ".pi/agent/models.json")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote Pi config")
	}
	if out, err := run("config", "apply"); err != nil || !bytes.Contains(out, []byte("API ready")) {
		t.Fatalf("%v %s", err, out)
	}
	calls, _ := os.ReadFile(record)
	if !strings.Contains(string(calls), "kickstart -k") || strings.Contains(string(calls), "bootout") {
		t.Fatalf("bad restart: %s", calls)
	}
	var piCfg struct {
		Providers map[string]struct {
			Models []struct {
				ID  string `json:"id"`
				Ctx int    `json:"contextWindow"`
			} `json:"models"`
		} `json:"providers"`
	}
	data, _ := os.ReadFile(filepath.Join(home, ".pi/agent/models.json"))
	if err := json.Unmarshal(data, &piCfg); err != nil {
		t.Fatal(err)
	}
	if len(piCfg.Providers["local"].Models) != 3 {
		t.Fatalf("aliases/persona missing: %s", data)
	}
	if out, err := run("models", "list"); err != nil || !bytes.Contains(out, []byte("persona")) || !bytes.Contains(out, []byte("fast")) {
		t.Fatalf("profile list: %v %s", err, out)
	}
	if out, err := run("models", "show", "persona"); err != nil || !bytes.Contains(out, []byte("65536")) {
		t.Fatalf("profile details: %v %s", err, out)
	}
	// Deleting shared files is rejected before any mutation.
	if out, err := run("models", "delete", "base", "--yes"); err == nil || !bytes.Contains(out, []byte("still use")) {
		t.Fatalf("%v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(modelDir, "model.gguf")); err != nil {
		t.Fatal("shared file deleted")
	}
	if out, err := run("models", "remove", "base"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	data, _ = os.ReadFile(state.DefaultConfigPath())
	if bytes.Contains(data, []byte("  base:")) || !bytes.Contains(data, []byte("persona:")) {
		t.Fatalf("incorrect removal: %s", data)
	}
	if out, err := run("models", "add", "--link", server.URL+"/new.gguf"); err != nil || !bytes.Contains(out, []byte("API ready")) {
		t.Fatalf("add link: %v %s", err, out)
	}
	data, _ = os.ReadFile(state.DefaultConfigPath())
	if !bytes.Contains(data, []byte("persona:")) || !bytes.Contains(data, []byte("--ctx-size 65536")) || !bytes.Contains(data, []byte("new:")) {
		t.Fatalf("add lost customization: %s", data)
	}
	// An auth failure must exit nonzero instead of claiming readiness.
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer badServer.Close()
	saved, _ := state.Load("")
	saved.Port = badServer.Listener.Addr().(*net.TCPAddr).Port
	if err := saved.Save(""); err != nil {
		t.Fatal(err)
	}
	if out, err := run("restart"); err == nil || bytes.Contains(out, []byte("API ready")) {
		t.Fatalf("false success: %v %s", err, out)
	}
}
