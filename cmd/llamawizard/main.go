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
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "status":
			runStatus()
			return
		case "start":
			runStart()
			return
		case "stop":
			runStop()
			return
		case "restart":
			runRestart()
			return
		case "doctor":
			runDoctor()
			return
		case "logs":
			runLogsCmd(os.Args[2:])
			return
		case "uninstall":
			runUninstall()
			return
		case "models":
			runModels(os.Args[2:])
			return
		case "config":
			runConfig(os.Args[2:])
			return
		case "pi":
			runPi(os.Args[2:])
			return
		case "version", "--version", "-v":
			fmt.Println("llamawizard", version)
			return
		case "update":
			runUpdate()
			return
		case "help", "-h", "--help":
			if len(os.Args) > 2 {
				printCommandHelp(os.Args[2])
			} else {
				printHelp()
			}
			return
		}
	}

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
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

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
	syscall.Exec(execPath, os.Args, os.Environ())
}

func printHelp() {
	fmt.Println(`llamawizard — local LLM stack manager

USAGE
  llamawizard [command] [flags]

  Running with no command launches the interactive setup wizard.

CORE
  status                    Show service status and health
  doctor                    Run a full health check
  logs [-f] [-n <N>]        Show recent service logs (-f follow, -n lines)

SERVICE
  start                     Start the llama-swap service
  stop                      Stop the llama-swap service
  restart                   Restart the llama-swap service

MODELS
  models list                        List configured models
  models add                         Add a model (interactive)
  models add --link <url> [--name]   Add a model from a direct download URL
  models show <name>                 Show a model's config and file path
  models remove <name>               Remove from config only (keeps file on disk)
  models delete <name> [--yes]       Remove from config AND delete the file

CONFIG
  config show                        Print the active config
  config path                        Print config file location

OPTIONAL
  pi install                         Install and configure pi coding agent
  pi uninstall                       Uninstall pi coding agent

MAINTENANCE
  update                    Check for and install updates
  uninstall                 Stop service and remove LaunchAgent
  version                   Show version
  help [command]            Show help (or help for a specific command)

Examples:
  llamawizard models add --link https://example.com/model.gguf --name qwen3
  llamawizard models delete qwen3 --yes
  llamawizard logs -f`)
}

func printCommandHelp(cmd string) {
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
	case "models":
		fmt.Println("llamawizard models <list|add|show|remove|delete> — Manage models.")
		fmt.Println("  models list                List configured models")
		fmt.Println("  models add                 Add a model interactively")
		fmt.Println("  models add --link <url>    Add from direct URL")
		fmt.Println("  models show <name>         Show model details")
		fmt.Println("  models remove <name>       Remove from config (keeps file)")
		fmt.Println("  models delete <name> --yes Remove config and delete file")
	case "config":
		fmt.Println("llamawizard config <show|path> — View configuration.")
		fmt.Println("  config show   Print the active llama-swap config")
		fmt.Println("  config path   Print config file location")
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
			"version": "Show version information",
			"help":    "Show this help",
		}[cmd])
	default:
		fmt.Printf("Unknown command: %s\nRun 'llamawizard help' for usage.\n", cmd)
	}
}
