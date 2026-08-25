package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eduard-lt/llamawizard/internal/update"
	"github.com/eduard-lt/llamawizard/internal/wizard"
)

var version = ""

func init() {
	if version != "" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			version = v
			return
		}
	}
	version = "dev"
}

func main() {
	// Normalize model slugs (ensure each includes its quantization) and repair
	// any duplicates. No-op unless changes are needed.
	if err := migrateSlugs(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: model slug migration failed: %v\n", err)
	}

	path, rest, wizard, err := resolve(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if wizard {
		runWizard()
		return
	}
	runResolved(path, rest)
}

// runWizard launches the interactive setup wizard, preceded by the
// non-blocking update check.
func runWizard() {
	checkForUpdates()

	p := tea.NewProgram(wizard.InitialModel(version), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func checkForUpdates() {
	release, err := update.CheckLatest()
	if err != nil {
		return
	}

	if !update.IsNewer(version, release.TagName) {
		return
	}

	fmt.Printf("\nNew version available: %s (you have %s)\n", release.TagName, version)
	fmt.Print("Update now? [Y/n] ")

	reader := bufio.NewReader(os.Stdin)
	input, readErr := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	if readErr != nil {
		// stdin closed before a line was read: nobody is there to answer
		// the prompt, so do not assume consent to a self-update.
		fmt.Println()
		fmt.Println("No interactive input on stdin — skipping update.")
		return
	}

	if input == "n" || input == "no" {
		fmt.Println()
		return
	}

	fmt.Println()
	if err := update.DownloadAndInstall(release); err != nil {
		fmt.Fprintf(os.Stderr, "Update failed: %v\n", err)
		fmt.Println("Starting wizard instead...")
		return
	}

	execPath, err := os.Executable()
	if err != nil {
		fmt.Printf("Updated to %s. Please restart.\n", release.TagName)
		os.Exit(0)
	}
	fmt.Printf("Updated to %s. Restarting...\n", release.TagName)
	if err := syscall.Exec(execPath, os.Args, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to restart: %v\nPlease restart manually.\n", err)
	}
}

func printHelp() {
	fmt.Println(`llamawizard — local LLM stack manager
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
  llamawizard completion zsh >> ~/.zshrc`)
}

// printCommandHelp prints help for a single command. It reports whether
// the command is known; unknown commands get an error (with a "Did you
// mean" suggestion when one is close) on stderr instead.
func printCommandHelp(cmd string) bool {
	switch cmd {
	case "status":
		fmt.Println("llamawizard status — Show service status, installed models, and health check.")
	case "doctor":
		fmt.Println("llamawizard doctor — Run a full health check against the running service.")
	case "logs":
		fmt.Println("llamawizard logs [-f] [-n <N>] — Show recent service logs.")
		fmt.Println("  -f    Follow the log (tail -f)")
		fmt.Println("  -n N  Show last N lines (default 30)")
	case "start", "stop", "restart":
		fmt.Printf("llamawizard %s — Manage the llama-swap LaunchAgent service.\n", cmd)
	case "warlock":
		fmt.Println("llamawizard warlock — Watch the llama-swap service with a live dashboard; restarts it automatically when it dies.")
		fmt.Println("  Opens LAN access (0.0.0.0) while warlock is open and restores loopback on exit.")
		fmt.Println("  --no-lan  Keep the loopback-only binding; do not open LAN access")
		fmt.Println("  q quit · r toggle auto-restart · a restart now")
	case "models":
		fmt.Println("llamawizard models <list|add|show|remove|delete> — Manage models.")
		fmt.Println("  models list (m ls)              List configured models")
		fmt.Println("  models add (m a)                Add a model interactively")
		fmt.Println("  models add --link <url>         Add from a link (direct .gguf or HF repo page)")
		fmt.Println("  models add --link               Open a guided tutorial for link formats")
		fmt.Println("  models show <name> (m sh)       Show model details")
		fmt.Println("  models remove <name> (m rm)     Remove from config (keeps file)")
		fmt.Println("  models delete <name> --yes      Remove config and delete file (no shorthand)")
	case "config":
		fmt.Println("llamawizard config <show|path> — View configuration.")
		fmt.Println("  config show (cfg sh)   Print the active llama-swap config")
		fmt.Println("  config path (cfg p)    Print config file location")
	case "completion":
		fmt.Println("llamawizard completion <bash|zsh|fish> — Print a shell completion script.")
		fmt.Println("  Append the output to your shell rc file, e.g.:")
		fmt.Println("    llamawizard completion zsh >> ~/.zshrc")
	case "pi":
		fmt.Println("llamawizard pi <install|uninstall> — Manage pi coding agent.")
		fmt.Println("  pi install    Install and configure pi for local models")
		fmt.Println("  pi uninstall  Uninstall pi")
	case "update":
		fmt.Println("llamawizard update — Check for and install the latest version.")
	case "uninstall":
		fmt.Println("llamawizard uninstall — Stop service, remove LaunchAgent, remove state file.")
	case "version", "help":
		fmt.Printf("llamawizard %s — %s\n", cmd, map[string]string{
			"version": "Show versions of llamawizard, llama.cpp, and llama-swap (N/A when not installed)",
			"help":    "Show this help",
		}[cmd])
	default:
		suggestion := didYouMean(cmd, commandNames(commandTree), 2)
		fmt.Fprintln(os.Stderr, unknownCommandError(
			fmt.Sprintf("command '%s'", cmd), suggestion, "help"))
		return false
	}
	return true
}
