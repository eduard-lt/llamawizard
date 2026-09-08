package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// wantHelp is the exact byte-for-byte target for `llamawizard help`
// (printHelp's raw string literal, plus Println's trailing newline).
const wantHelp = `llamawizard — local LLM stack manager
USAGE
  llamawizard [command] [flags]
  Running with NO arguments launches the interactive setup wizard.
  Unrecognized commands print an error + suggestion — they never fall through to the wizard.

CORE
  status, st                 Show service status and health
  doctor, dr                 Run a full health check
  logs, lg [-f] [-n <N>]     Show recent service logs (-f follow, -n lines)

SERVICE
  start                      Start the llama-swap service
  stop                       Stop the llama-swap service
  restart, re                Restart the llama-swap service

GUARD
  warlock, wl [--no-lan]     Live dashboard + auto-restart; LAN access while open

MODELS  (alias: m)
  models list, m ls                      List configured models
  models add, m a                        Add a model (interactive)
  models add --link <url> [name]         Add a model from a link (file or repo page)
  models add --link                      Open a guided tutorial for link formats
  models show <name>, m sh <name>        Show a model's config and file path
  models remove <name>, m rm <name>      Remove from config only (keeps file on disk)
  models delete <name> [--yes]           Remove from config AND delete the file (no shorthand — destructive)

CONFIG  (alias: cfg)
  config apply [--dry-run] [--default ID] Sync master config and verify service readiness
  config show, cfg sh                    Print the active config
  config path, cfg p                     Print config file location

OPTIONAL
  pi install                 Install and configure pi coding agent
  pi uninstall                Uninstall pi coding agent

SHELL
  completion <bash|zsh|fish>  Print shell completion script (see docs for setup)

MAINTENANCE
  update, up                 Check for and install updates
  uninstall                  Stop service and remove LaunchAgent (no shorthand — destructive)
  version, v                 Show versions (llamawizard, llama.cpp, llama-swap)
  help, h [command]          Show help (or help for a specific command)

Examples:
  llamawizard m a --link https://huggingface.co/unsloth/Qwen3.8-27B-GGUF
  llamawizard m a --link https://example.com/model.gguf my-model
  llamawizard models delete qwen3 --yes
  llamawizard lg -f
  llamawizard completion zsh >> ~/.zshrc
`

// captureHelp runs fn with os.Stdout redirected to a pipe and returns
// everything written to it. The original stdout is restored before
// returning so test output is unaffected.
func captureHelp(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stdout pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()

	_ = w.Close()
	<-done
	os.Stdout = old
	return buf.String()
}

func TestPrintHelpExact(t *testing.T) {
	got := captureHelp(t, printHelp)
	if got != wantHelp {
		t.Errorf("printHelp output differs from target:\n--- got ---\n%s\n--- want ---\n%s", got, wantHelp)
	}
}

func TestHelpCoversCommandTree(t *testing.T) {
	help := captureHelp(t, printHelp)

	// Every top-level command name and alias appears in the help.
	for _, c := range commandTree {
		if !strings.Contains(help, c.Name) {
			t.Errorf("help output missing top-level command %q", c.Name)
		}
		for _, a := range c.Aliases {
			if !strings.Contains(help, a) {
				t.Errorf("help output missing alias %q for %q", a, c.Name)
			}
		}
	}

	// Every child of a noun command appears in its "models X, m Y" style
	// line: on a line of the noun's section (e.g. "models list, m ls" or
	// "completion <bash|zsh|fish>"), plus the short "alias child" form
	// when both the noun and the child have aliases.
	lines := strings.Split(help, "\n")
	for _, noun := range []string{"models", "config", "pi", "completion"} {
		var node *command
		for _, c := range commandTree {
			if c.Name == noun {
				node = c
				break
			}
		}
		if node == nil {
			t.Fatalf("commandTree has no %q node", noun)
		}
		var nounAlias string
		if len(node.Aliases) > 0 {
			nounAlias = node.Aliases[0]
		}
		for _, child := range node.Children {
			found := false
			for _, line := range lines {
				if strings.Contains(line, noun) && strings.Contains(line, child.Name) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("help output has no %s line mentioning %q", noun, child.Name)
			}
			for _, a := range child.Aliases {
				if nounAlias == "" {
					continue // no short form exists for this noun
				}
				if !strings.Contains(help, nounAlias+" "+a) {
					t.Errorf("help output missing %q line for %s %s", nounAlias+" "+a, noun, child.Name)
				}
			}
		}
	}
}
