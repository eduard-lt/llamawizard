package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/eduard-lt/llamawizard/internal/health"
	"github.com/eduard-lt/llamawizard/internal/state"
)

func TestRestartRequiresHealthyAPI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(state.DefaultConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.DefaultConfigPath(), []byte("apiKeys: [custom]\nmodels:\n  persona:\n    cmd: /server\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldRestart, oldCheck := restart, check
	t.Cleanup(func() { restart = oldRestart; check = oldCheck })
	restart = func(string) error { return nil }
	check = func(port int, ids []string, key string) (health.Report, error) {
		if port != 9090 || key != "custom" || len(ids) != 1 || ids[0] != "persona" {
			t.Fatalf("bad check: %d %v %s", port, ids, key)
		}
		return health.Report{Error: "connection refused"}, nil
	}
	st := &state.State{Port: 9090}
	if err := RestartAndCheck(st); err == nil {
		t.Fatal("reported success for offline service")
	}
	check = func(int, []string, string) (health.Report, error) { return health.Report{Pass: true}, nil }
	if err := RestartAndCheck(st); err != nil {
		t.Fatal(err)
	}
	restart = func(string) error { return fmt.Errorf("bootstrap failed") }
	if err := RestartAndCheck(st); err == nil {
		t.Fatal("ignored launchd failure")
	}
}
