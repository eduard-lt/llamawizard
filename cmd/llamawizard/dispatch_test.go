package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sameStrings reports whether a and b hold the same elements, treating a
// nil slice and an empty slice as equal.
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestResolve_ZeroArgs_LaunchesWizard(t *testing.T) {
	path, rest, wizard, err := resolve([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !wizard {
		t.Error("wizard = false, want true for zero args")
	}
	if path != "" {
		t.Errorf("path = %q, want empty", path)
	}
	if rest != nil {
		t.Errorf("rest = %v, want nil", rest)
	}
}

// The regression: `model add` (missing the "s") used to fall through to the
// interactive wizard. It must now be rejected with a suggestion.
func TestResolve_UnknownCommand_SuggestsClosest(t *testing.T) {
	_, _, wizard, err := resolve([]string{"model", "add"})
	if err == nil {
		t.Fatal("expected error for unknown command 'model', got nil")
	}
	if wizard {
		t.Error("wizard = true, want false for unknown command")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unknown command 'model'") {
		t.Errorf("error %q does not contain unknown command 'model'", msg)
	}
	if !strings.Contains(msg, "Did you mean 'models'?") {
		t.Errorf("error %q does not contain Did you mean 'models'?", msg)
	}
}

func TestResolve_UnknownCommand_NoSuggestion(t *testing.T) {
	_, _, _, err := resolve([]string{"xyzzy"})
	if err == nil {
		t.Fatal("expected error for unknown command 'xyzzy', got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unknown command 'xyzzy'") {
		t.Errorf("error %q does not contain unknown command 'xyzzy'", msg)
	}
	if strings.Contains(msg, "Did you mean") {
		t.Errorf("error %q has a suggestion, want none for 'xyzzy'", msg)
	}
}

func TestResolve_UnknownSubcommand(t *testing.T) {
	cases := []struct {
		args      []string
		wantErr   string
		wantSugg  string
		noSuggest bool
	}{
		{
			args:     []string{"models", "ad"},
			wantErr:  "unknown subcommand 'models ad'",
			wantSugg: "Did you mean 'models add'?",
		},
		{
			args:     []string{"config", "sho"},
			wantErr:  "unknown subcommand 'config sho'",
			wantSugg: "Did you mean 'config show'?",
		},
		{
			args:      []string{"models", "--link"},
			wantErr:   "unknown subcommand 'models --link'",
			noSuggest: true,
		},
	}
	for _, c := range cases {
		_, _, wizard, err := resolve(c.args)
		if err == nil {
			t.Fatalf("resolve(%v): expected error, got nil", c.args)
		}
		if wizard {
			t.Errorf("resolve(%v): wizard = true, want false", c.args)
		}
		msg := err.Error()
		if !strings.Contains(msg, c.wantErr) {
			t.Errorf("resolve(%v): error %q does not contain %q", c.args, msg, c.wantErr)
		}
		if c.wantSugg != "" && !strings.Contains(msg, c.wantSugg) {
			t.Errorf("resolve(%v): error %q does not contain %q", c.args, msg, c.wantSugg)
		}
		if c.noSuggest && strings.Contains(msg, "Did you mean") {
			t.Errorf("resolve(%v): error %q has a suggestion, want none", c.args, msg)
		}
	}
}

func TestResolve_CanonicalForms(t *testing.T) {
	cases := []struct {
		args     []string
		wantPath string
		wantRest []string
	}{
		{[]string{"status"}, "status", nil},
		{[]string{"start"}, "start", nil},
		{[]string{"stop"}, "stop", nil},
		{[]string{"restart"}, "restart", nil},
		{[]string{"warlock", "--no-lan"}, "warlock", []string{"--no-lan"}},
		{[]string{"doctor"}, "doctor", nil},
		{[]string{"logs", "-f", "-n", "10"}, "logs", []string{"-f", "-n", "10"}},
		{[]string{"models", "list"}, "models.list", nil},
		{[]string{"models", "add", "--link", "u", "n"}, "models.add", []string{"--link", "u", "n"}},
		{[]string{"models", "show", "x"}, "models.show", []string{"x"}},
		{[]string{"models", "remove", "x"}, "models.remove", []string{"x"}},
		{[]string{"models", "delete", "x", "--yes"}, "models.delete", []string{"x", "--yes"}},
		{[]string{"config", "show"}, "config.show", nil},
		{[]string{"config", "path"}, "config.path", nil},
		{[]string{"pi", "install"}, "pi.install", nil},
		{[]string{"pi", "uninstall"}, "pi.uninstall", nil},
		{[]string{"version"}, "version", nil},
		{[]string{"update"}, "update", nil},
		{[]string{"uninstall"}, "uninstall", nil},
		{[]string{"help"}, "help", nil},
		{[]string{"help", "models"}, "help", []string{"models"}},
		{[]string{"--version"}, "version", nil},
		{[]string{"-v"}, "version", nil},
		{[]string{"-h"}, "help", nil},
		{[]string{"--help"}, "help", nil},
	}
	for _, c := range cases {
		path, rest, wizard, err := resolve(c.args)
		if err != nil {
			t.Errorf("resolve(%v): unexpected error: %v", c.args, err)
			continue
		}
		if wizard {
			t.Errorf("resolve(%v): wizard = true, want false", c.args)
		}
		if path != c.wantPath {
			t.Errorf("resolve(%v): path = %q, want %q", c.args, path, c.wantPath)
		}
		if !sameStrings(rest, c.wantRest) {
			t.Errorf("resolve(%v): rest = %v, want %v", c.args, rest, c.wantRest)
		}
	}
}

func TestResolve_NounWithoutSubcommand(t *testing.T) {
	path, rest, wizard, err := resolve([]string{"models"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wizard {
		t.Error("wizard = true, want false")
	}
	if path != "models" {
		t.Errorf("path = %q, want %q", path, "models")
	}
	if len(rest) != 0 {
		t.Errorf("rest = %v, want empty", rest)
	}
}

// buildTestBinary compiles the CLI so the test can exercise the real
// resolve -> runResolved -> run-function chain end to end.
func buildTestBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "llamawizard")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("building test binary: %v\n%s", err, out)
	}
	return bin
}

// Integration counterpart of TestResolve_NounWithoutSubcommand: when a noun
// with subcommands is given bare (e.g. `llamawizard models`), resolve
// returns (noun, [], false, nil) and runResolved dispatches to the noun's
// run function with an empty rest. That function must print its usage and
// exit 1 — a bare noun must never panic or fall through to the wizard.
func TestNounWithoutSubcommand_PrintsUsageAndExits(t *testing.T) {
	bin := buildTestBinary(t)
	home := t.TempDir() // isolate state/config so the binary touches nothing real

	cases := []struct {
		args    []string
		wantOut string // substring that must appear on stdout
		wantErr string // substring that must appear on stderr
	}{
		{[]string{"models"}, "Usage: llamawizard models <list|add|show|remove|delete>", ""},
		{[]string{"m"}, "Usage: llamawizard models <list|add|show|remove|delete>", ""},
		{[]string{"config"}, "Usage: llamawizard config <show|path|apply>", ""},
		{[]string{"cfg"}, "Usage: llamawizard config <show|path|apply>", ""},
		{[]string{"pi"}, "Usage: llamawizard pi <install|uninstall>", ""},
		{[]string{"completion"}, "", "Usage: llamawizard completion <bash|zsh|fish>"},
	}

	for _, c := range cases {
		t.Run(strings.Join(c.args, "_"), func(t *testing.T) {
			cmd := exec.Command(bin, c.args...)
			cmd.Env = append(os.Environ(), "HOME="+home)
			var out, errb bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &errb
			err := cmd.Run()

			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("expected clean exit, got %v\nstdout: %s\nstderr: %s", err, out.String(), errb.String())
			}
			// A panic would surface as a non-1 exit code (2) with a stack
			// trace, so asserting exactly 1 is the no-panic guarantee.
			if exitErr.ExitCode() != 1 {
				t.Fatalf("exit code = %d, want 1\nstdout: %s\nstderr: %s", exitErr.ExitCode(), out.String(), errb.String())
			}
			if c.wantOut != "" && !strings.Contains(out.String(), c.wantOut) {
				t.Errorf("stdout %q missing %q", out.String(), c.wantOut)
			}
			if c.wantErr != "" && !strings.Contains(errb.String(), c.wantErr) {
				t.Errorf("stderr %q missing %q", errb.String(), c.wantErr)
			}
		})
	}
}

func TestHelpFlagsNeverDispatchAnAction(t *testing.T) {
	for _, args := range [][]string{{"-help"}, {"start", "--help"}, {"models", "-h"}, {"models", "delete", "base", "--help"}, {"models", "add", "--link", "https://example.com/model.gguf", "--help"}, {"cfg", "apply", "--dry-run", "-help"}} {
		path, _, wizard, err := resolve(args)
		if err != nil || wizard || path != "help" {
			t.Fatalf("%v dispatched %q wizard=%v err=%v", args, path, wizard, err)
		}
	}
}
