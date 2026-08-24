package main

import (
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
