package configsync

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/eduard-lt/llamawizard/internal/llamaswap"
	"github.com/eduard-lt/llamawizard/internal/state"
)

const customized = `# My tuning notes
apiKeys: [secret]
healthCheckTimeout: 900
models:
  base:
    name: Base
    cmd: |
      '/server'
      --model '/models/base.gguf'
      --ctx-size 98304
      --cache-type-k q8_0
    aliases: [fast]
    ttl: 300
  corp-ceo:
    name: CEO
    cmd: |
      '/server' --model '/models/base.gguf' --ctx-size 65536
`

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
func readTest(t *testing.T, path string) []byte {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func setup(t *testing.T) *state.State {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	st := &state.State{Port: 9091, APIKey: "stale", LlamaCppPath: "/server", PiConfigured: true, Models: []state.ModelEntry{{Slug: "base", File: "base.gguf"}}}
	if err := st.Save(""); err != nil {
		t.Fatal(err)
	}
	write(t, state.DefaultConfigPath(), []byte(customized))
	home, _ := os.UserHomeDir()
	write(t, filepath.Join(home, ".pi/agent/models.json"), []byte(`{"extensionSetting":true,"providers":{"external":{"headers":{"x-test":"keep"},"models":[{"id":"other","reasoning":true}]},"local":{"compat":{"supportsDeveloperRole":false},"models":[{"id":"base","maxTokens":1234,"reasoning":true},{"id":"pi-only","contextWindow":2048}]}}}`))
	write(t, filepath.Join(home, ".pi/agent/settings.json"), []byte(`{"theme":"dark","defaultProvider":"external","defaultModel":"other","enabledModels":["external/*","local/corp-*"],"compaction":{"reserveTokens":4096}}`))
	return st
}
func TestAddPreservesMasterAndPiCustomizations(t *testing.T) {
	st := setup(t)
	home, _ := os.UserHomeDir()
	settingsPath := filepath.Join(home, ".pi/agent/settings.json")
	st.Models = append(st.Models, state.ModelEntry{Slug: "new", File: "new.gguf", CtxSize: 32768})
	plan, err := Prepare(st, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Preparing is read-only.
	if string(readTest(t, state.DefaultConfigPath())) != customized {
		t.Fatal("prepare wrote master")
	}
	backup, err := plan.Commit()
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("missing backup")
	}
	var before, after map[string]any
	if err = yaml.Unmarshal([]byte(customized), &before); err != nil {
		t.Fatal(err)
	}
	if err = yaml.Unmarshal(readTest(t, state.DefaultConfigPath()), &after); err != nil {
		t.Fatal(err)
	}
	bm := before["models"].(map[string]any)
	am := after["models"].(map[string]any)
	for _, id := range []string{"base", "corp-ceo"} {
		a, _ := json.Marshal(bm[id])
		b, _ := json.Marshal(am[id])
		if !bytes.Equal(a, b) {
			t.Fatalf("%s customization lost", id)
		}
	}
	if len(am) != 3 || after["healthCheckTimeout"] != 900 {
		t.Fatalf("bad merged master: %v", after)
	}
	if !bytes.Contains(readTest(t, state.DefaultConfigPath()), []byte("# My tuning notes")) {
		t.Fatal("comment lost")
	}
	var piCfg map[string]any
	if err := json.Unmarshal(readTest(t, filepath.Join(home, ".pi/agent/models.json")), &piCfg); err != nil {
		t.Fatal(err)
	}
	providers := piCfg["providers"].(map[string]any)
	local := providers["local"].(map[string]any)
	if local["apiKey"] != "secret" || local["baseUrl"] != "http://127.0.0.1:9091/v1" {
		t.Fatal(local)
	}
	if providers["external"] == nil || local["compat"] == nil || piCfg["extensionSetting"] != true {
		t.Fatal("unknown Pi fields lost")
	}
	entries := map[string]map[string]any{}
	for _, e := range local["models"].([]any) {
		m := e.(map[string]any)
		entries[m["id"].(string)] = m
	}
	if entries["base"]["contextWindow"] != float64(98304) || entries["base"]["maxTokens"] != float64(1234) || entries["base"]["reasoning"] != true {
		t.Fatal(entries["base"])
	}
	if entries["corp-ceo"]["contextWindow"] != float64(65536) || entries["fast"] == nil || entries["pi-only"] == nil {
		t.Fatal(entries)
	}
	var settings map[string]any
	if err := json.Unmarshal(readTest(t, settingsPath), &settings); err != nil {
		t.Fatal(err)
	}
	if settings["defaultModel"] != "other" || settings["theme"] != "dark" || len(settings["enabledModels"].([]any)) != 2 {
		t.Fatal(settings)
	}
	saved, err := state.Load("")
	if err != nil || saved.APIKey != "secret" || len(saved.Models) != 2 {
		t.Fatalf("state: %+v %v", saved, err)
	}
	second, err := Prepare(saved, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Changes) != 0 {
		t.Fatalf("not idempotent: %+v", second.Changes)
	}
}
func TestInvalidPiDoesNotChangeAnyFiles(t *testing.T) {
	st := setup(t)
	home, _ := os.UserHomeDir()
	write(t, filepath.Join(home, ".pi/agent/settings.json"), []byte(`{"broken"`))
	before := readTest(t, state.DefaultPath())
	if _, err := Prepare(st, Options{}); err == nil {
		t.Fatal("accepted invalid Pi JSON")
	}
	if !bytes.Equal(before, readTest(t, state.DefaultPath())) || string(readTest(t, state.DefaultConfigPath())) != customized {
		t.Fatal("partial write")
	}
}
func TestConcurrentEditAbortsCommit(t *testing.T) {
	st := setup(t)
	plan, err := Prepare(st, Options{})
	if err != nil {
		t.Fatal(err)
	}
	original := readTest(t, state.DefaultConfigPath())
	write(t, state.DefaultConfigPath(), append(original, []byte("\n# concurrent edit\n")...))
	if _, err = plan.Commit(); err == nil {
		t.Fatal("overwrote concurrent change")
	}
	if !bytes.Contains(readTest(t, state.DefaultConfigPath()), []byte("concurrent edit")) {
		t.Fatal("lost edit")
	}
}
func TestApplyRemovesOnlyPreviouslyManagedPiIDs(t *testing.T) {
	st := setup(t)
	plan, err := Prepare(st, Options{ApplyOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = plan.Commit(); err != nil {
		t.Fatal(err)
	}
	master, err := llamaswap.Merge([]byte(customized), []byte("models: {}\n"), []string{"corp-ceo"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	write(t, state.DefaultConfigPath(), master)
	plan, err = Prepare(st, Options{ApplyOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = plan.Commit(); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	data := readTest(t, filepath.Join(home, ".pi/agent/models.json"))
	if bytes.Contains(data, []byte(`"id": "corp-ceo"`)) || !bytes.Contains(data, []byte(`"id": "pi-only"`)) {
		t.Fatalf("incorrect pruning: %s", data)
	}
}
func TestExplicitKeyUpdatesMasterAndPi(t *testing.T) {
	st := setup(t)
	key := "new-secret"
	plan, err := Prepare(st, Options{APIKey: &key})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = plan.Commit(); err != nil {
		t.Fatal(err)
	}
	_, got, err := llamaswap.Catalog(readTest(t, state.DefaultConfigPath()))
	if err != nil || got != key {
		t.Fatalf("key %q %v", got, err)
	}
	home, _ := os.UserHomeDir()
	if !bytes.Contains(readTest(t, filepath.Join(home, ".pi/agent/models.json")), []byte(key)) {
		t.Fatal("Pi key not synced")
	}
}
func TestWriteFailureRollsBackAlreadyReplacedFiles(t *testing.T) {
	st := setup(t)
	plan, err := Prepare(st, Options{})
	if err != nil {
		t.Fatal(err)
	}
	old := replaceFile
	t.Cleanup(func() { replaceFile = old })
	calls := 0
	replaceFile = func(path string, data []byte, mode os.FileMode) error {
		calls++
		if calls == 2 {
			return os.ErrPermission
		}
		return old(path, data, mode)
	}
	backup, err := plan.Commit()
	if err == nil || backup == "" {
		t.Fatalf("%s %v", backup, err)
	}
	for _, c := range plan.Changes {
		got, exists, _, e := read(c.Path)
		if e != nil || exists != c.Exists || !bytes.Equal(got, c.Before) {
			t.Fatalf("rollback failed for %s: %v", c.Path, e)
		}
	}
}
