package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var (
	completionTopLevelWords = []string{
		"status", "st", "doctor", "dr", "logs", "lg", "start", "stop",
		"restart", "re", "warlock", "wl", "models", "m", "config", "cfg",
		"pi", "completion", "update", "up", "uninstall", "version", "v", "help", "h",
		"--help", "-h", "--version", "-v",
	}
	completionSubcommandWords = []string{
		"list", "ls", "add", "a", "show", "sh", "remove", "rm", "delete",
		"path", "p", "install", "uninstall", "bash", "zsh", "fish",
	}
	completionFlagWords = []string{
		"-f", "-n", "--no-lan", "--link", "--name", "--yes",
	}
)

func TestCompletionFor_AllShells(t *testing.T) {
	words := make([]string, 0,
		len(completionTopLevelWords)+len(completionSubcommandWords)+len(completionFlagWords))
	words = append(words, completionTopLevelWords...)
	words = append(words, completionSubcommandWords...)
	words = append(words, completionFlagWords...)

	for _, shell := range []string{"bash", "zsh", "fish"} {
		script, err := completionFor(shell)
		if err != nil {
			t.Fatalf("completionFor(%q): unexpected error: %v", shell, err)
		}
		if script == "" {
			t.Fatalf("completionFor(%q): empty script", shell)
		}
		for _, w := range words {
			if !strings.Contains(script, w) {
				t.Errorf("completionFor(%q): script missing word %q", shell, w)
			}
		}
	}

	if _, err := completionFor("powershell"); err == nil {
		t.Error(`completionFor("powershell"): expected error, got nil`)
	}
}

func TestCompletionFor_BashSyntax(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	script, err := completionFor("bash")
	if err != nil {
		t.Fatalf("completionFor(bash): %v", err)
	}
	path := filepath.Join(t.TempDir(), "completion.bash")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatalf("writing script: %v", err)
	}
	if out, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("bash -n failed: %v\n%s", err, out)
	}
}

func TestCompletionFor_BashFunctional(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	script, err := completionFor("bash")
	if err != nil {
		t.Fatalf("completionFor(bash): %v", err)
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "completion.bash")
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatalf("writing script: %v", err)
	}

	driver := `#!/usr/bin/env bash
source "$1"

run_case() {
    COMP_WORDS=("$@")
    COMP_CWORD=$(( ${#COMP_WORDS[@]} - 1 ))
    COMPREPLY=()
    _llamawizard
    printf '%s\n' "${COMPREPLY[*]}"
}

run_case llamawizard ""
run_case llamawizard models ""
run_case llamawizard logs ""
run_case llamawizard m a ""
`
	driverPath := filepath.Join(dir, "driver.sh")
	if err := os.WriteFile(driverPath, []byte(driver), 0o755); err != nil {
		t.Fatalf("writing driver: %v", err)
	}

	out, err := exec.Command("bash", driverPath, scriptPath).CombinedOutput()
	if err != nil {
		t.Fatalf("bash driver failed: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 4 {
		t.Fatalf("driver printed %d lines, want 4:\n%s", len(lines), out)
	}

	checkContains := func(line int, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(lines[line], w) {
				t.Errorf("case %d output %q missing %q", line+1, lines[line], w)
			}
		}
	}
	checkContains(0, "models", "m", "status", "st", "completion", "--version")
	checkContains(1, "list", "ls", "add", "a", "show", "sh", "remove", "rm", "delete")
	checkContains(2, "-f", "-n")
	checkContains(3, "--link", "--name")
}

func TestCompletionFor_ZshSyntax(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not installed")
	}
	script, err := completionFor("zsh")
	if err != nil {
		t.Fatalf("completionFor(zsh): %v", err)
	}
	path := filepath.Join(t.TempDir(), "completion.zsh")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatalf("writing script: %v", err)
	}
	if out, err := exec.Command("zsh", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("zsh -n failed: %v\n%s", err, out)
	}
}

func TestCompletionFor_ZshFunctional(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not installed")
	}
	script, err := completionFor("zsh")
	if err != nil {
		t.Fatalf("completionFor(zsh): %v", err)
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "completion.zsh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatalf("writing script: %v", err)
	}

	driver := `#!/usr/bin/env zsh
compdef() { :; }
source "$1"

words=(llamawizard "")
CURRENT=2
_llamawizard
print -r -- "${reply[*]}"

words=(llamawizard models "")
CURRENT=3
_llamawizard
print -r -- "${reply[*]}"
`
	driverPath := filepath.Join(dir, "driver.zsh")
	if err := os.WriteFile(driverPath, []byte(driver), 0o755); err != nil {
		t.Fatalf("writing driver: %v", err)
	}

	out, err := exec.Command("zsh", driverPath, scriptPath).CombinedOutput()
	if err != nil {
		t.Fatalf("zsh driver failed: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		t.Fatalf("driver printed %d lines, want 2:\n%s", len(lines), out)
	}

	checkContains := func(line int, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(lines[line], w) {
				t.Errorf("case %d output %q missing %q", line+1, lines[line], w)
			}
		}
	}
	checkContains(0, "models", "m", "status", "st", "completion", "--version")
	checkContains(1, "list", "ls", "add", "a", "show", "sh", "remove", "rm", "delete")
}

func TestCompletionFor_FishSyntax(t *testing.T) {
	if _, err := exec.LookPath("fish"); err != nil {
		t.Skip("fish not installed")
	}
	script, err := completionFor("fish")
	if err != nil {
		t.Fatalf("completionFor(fish): %v", err)
	}
	path := filepath.Join(t.TempDir(), "completion.fish")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatalf("writing script: %v", err)
	}
	if out, err := exec.Command("fish", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("fish -n failed: %v\n%s", err, out)
	}
}
