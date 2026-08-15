package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eduard-lt/llamawizard/internal/hardware"
	"github.com/eduard-lt/llamawizard/internal/health"
	"github.com/eduard-lt/llamawizard/internal/launchd"
	"github.com/eduard-lt/llamawizard/internal/llamaswap"
	"github.com/eduard-lt/llamawizard/internal/pi"
	"github.com/eduard-lt/llamawizard/internal/state"
	"github.com/eduard-lt/llamawizard/internal/update"
	"github.com/eduard-lt/llamawizard/internal/wizard"
)

func runStatus() {
	st, err := state.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading state: %v\n", err)
		os.Exit(1)
	}

	if st.Port == 0 {
		fmt.Println("llamawizard has not been set up yet. Run 'llamawizard install' first.")
		os.Exit(1)
	}

	fmt.Println("=== llamawizard status ===")
	fmt.Printf("  Port:        %d\n", st.Port)
	fmt.Printf("  API Key:     %s\n", st.APIKey)
	fmt.Printf("  Chip:        %s\n", st.Chip)
	fmt.Printf("  llama.cpp:   %s\n", st.LlamaCppPath)
	fmt.Printf("  llama-swap:  %s\n", st.LlamaSwapPath)
	fmt.Printf("  Models:      %d installed\n", len(st.Models))
	for _, m := range st.Models {
		fmt.Printf("    - %s (%s) %s\n", m.Slug, m.Quant, m.Name)
	}
	if st.PiConfigured {
		fmt.Println("  pi:          configured")
	}
	if st.LastHealthCheck != nil {
		fmt.Printf("  Last check:  %s\n", st.LastHealthCheck.Format(time.RFC3339))
	}

	fmt.Println()

	status, err := launchd.Status()
	if err != nil {
		fmt.Printf("Service: unable to determine (error: %v)\n", err)
	} else {
		running := strings.Contains(status, "state = running")
		if running {
			fmt.Println("Service: running")
		} else {
			fmt.Println("Service: not running")
		}
	}

	var modelIDs []string
	for _, m := range st.Models {
		modelIDs = append(modelIDs, m.Slug)
	}

	if len(modelIDs) > 0 {
		fmt.Print("\nHealth check: ")
		r, err := health.CheckWithKey(st.Port, modelIDs, st.APIKey)
		if err != nil {
			fmt.Printf("FAIL (error: %v)\n", err)
			return
		}
		if r.Pass {
			fmt.Printf("PASS (%d models, %d attempts, %s)\n", len(r.FoundModels), r.Attempts, r.Duration)
		} else {
			fmt.Printf("FAIL (%s)\n", r.Error)
			if len(r.MissingModels) > 0 {
				fmt.Printf("  Missing: %v\n", r.MissingModels)
			}
		}
	}
}

func runStart() {
	plistPath, err := defaultPlistPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Plist not found at %s. Run 'llamawizard install' first.\n", plistPath)
		os.Exit(1)
	}

	if err := launchd.Start(plistPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting service: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Service started.")
}

func runStop() {
	if err := launchd.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping service: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Service stopped.")
}

func runRestart() {
	if err := launchd.Stop(); err != nil {
		log.Printf("Warning: failed to stop service: %v", err)
	}

	plistPath, err := defaultPlistPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := launchd.Start(plistPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Service restarted.")
}

func runDoctor() {
	st, err := state.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading state: %v\n", err)
		os.Exit(1)
	}

	if st.Port == 0 {
		fmt.Println("llamawizard has not been set up yet. Run 'llamawizard install' first.")
		os.Exit(1)
	}

	var modelIDs []string
	for _, m := range st.Models {
		modelIDs = append(modelIDs, m.Slug)
	}

	fmt.Println("=== llamawizard doctor ===")
	fmt.Printf("Port: %d\n", st.Port)
	fmt.Printf("Expected models: %v\n", modelIDs)
	fmt.Println()

	r, _ := health.CheckWithKey(st.Port, modelIDs, st.APIKey)

	if r.Pass {
		fmt.Printf("PASS — all %d models healthy (%d attempts, %s)\n",
			len(r.FoundModels), r.Attempts, r.Duration)
		now := time.Now()
		st.LastHealthCheck = &now
		_ = st.Save("")
	} else {
		fmt.Println("FAIL")
		if r.Error != "" {
			fmt.Printf("  Error: %s\n", r.Error)
		}
		if len(r.MissingModels) > 0 {
			fmt.Printf("  Missing: %v\n", r.MissingModels)
		}
		if r.ErrorLogTail != "" {
			fmt.Printf("\n  Error log tail (%s):\n", r.ErrorLogPath)
			for _, line := range strings.Split(r.ErrorLogTail, "\n") {
				fmt.Printf("    %s\n", line)
			}
		}
		os.Exit(1)
	}
}

func runLogsCmd(args []string) {
	follow := false
	numLines := 30

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-f":
			follow = true
		case "-n":
			if i+1 < len(args) {
				i++
				if _, err := fmt.Sscanf(args[i], "%d", &numLines); err != nil {
					numLines = 30
				}
			}
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	errLog := filepath.Join(home, ".local", "ai", "logs", "llama-swap-error.log")
	stdLog := filepath.Join(home, ".local", "ai", "logs", "llama-swap.log")

	if follow {
		cmd := exec.Command("tail", "-f", "-n", fmt.Sprintf("%d", numLines), stdLog)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
		return
	}

	fmt.Printf("=== error log (%s) ===\n", errLog)
	printTail(errLog, numLines)

	fmt.Printf("\n=== stdout log (%s) ===\n", stdLog)
	printTail(stdLog, numLines)

	fmt.Printf("\n(use -f to follow: llamawizard logs -f)\n")
}

func runUninstall() {
	st, err := state.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading state: %v\n", err)
		os.Exit(1)
	}

	if st.Port == 0 {
		fmt.Println("Nothing to uninstall — llamawizard has not been set up.")
		return
	}

	fmt.Println("=== llamawizard uninstall ===")
	fmt.Println("This will:")
	fmt.Println("  1. Stop the llama-swap service")
	fmt.Println("  2. Remove the LaunchAgent plist")
	fmt.Printf("  3. Leave %d model files on disk (~/.local/ai/ and ~/models/ are NOT deleted)\n", len(st.Models))
	fmt.Print("\nProceed? [y/N] ")

	var answer string
	_, _ = fmt.Scanln(&answer)
	if strings.ToLower(answer) != "y" && strings.ToLower(answer) != "yes" {
		fmt.Println("Cancelled.")
		return
	}

	if err := launchd.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: stop may have failed: %v\n", err)
	}

	plistPath, err := defaultPlistPath()
	if err == nil {
		if err := launchd.Uninstall(plistPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: uninstall may have failed: %v\n", err)
		}
	}

	statePath := state.DefaultPath()
	if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: could not remove state.json: %v\n", err)
	}

	fmt.Println("\nUninstall complete.")
	fmt.Println("Model files in ~/models/ were left untouched. Delete them manually if desired.")
	fmt.Println("Config at ~/.local/ai/config/ was left untouched.")
}

func runUpdate() {
	fmt.Println("Checking for updates...")

	release, err := update.CheckLatest()
	if err != nil {
		if errors.Is(err, update.ErrNoReleases) {
			fmt.Println("No releases available yet.")
			return
		}
		fmt.Fprintf(os.Stderr, "Error checking for updates: %v\n", err)
		os.Exit(1)
	}

	if !update.IsNewer(version, release.TagName) {
		fmt.Printf("Already up to date (%s)\n", version)
		return
	}

	fmt.Printf("\nCurrent version: %s\n", version)
	fmt.Printf("Latest version:  %s\n", release.TagName)
	fmt.Print("\nUpdate now? [Y/n] ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "n" || input == "no" {
		fmt.Println("Update cancelled.")
		return
	}

	fmt.Println()
	if err := update.DownloadAndInstall(release); err != nil {
		fmt.Fprintf(os.Stderr, "Update failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nSuccessfully updated to %s!\n", release.TagName)
	fmt.Println("Run 'llamawizard restart' if the service was running.")
}

func runModels(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: llamawizard models <list|add|show|remove|delete>")
		os.Exit(1)
	}

	switch args[0] {
	case "list":
		runModelsList()
	case "add":
		runModelsAdd(args[1:])
	case "show":
		if len(args) < 2 {
			fmt.Println("Usage: llamawizard models show <name>")
			os.Exit(1)
		}
		runModelsShow(args[1])
	case "remove":
		if len(args) < 2 {
			fmt.Println("Usage: llamawizard models remove <name>")
			os.Exit(1)
		}
		runModelsRemove(args[1])
	case "delete":
		if len(args) < 2 {
			fmt.Println("Usage: llamawizard models delete <name> [--yes]")
			os.Exit(1)
		}
		yesFlag := len(args) > 2 && args[2] == "--yes"
		runModelsDelete(args[1], yesFlag)
	default:
		fmt.Printf("Unknown subcommand: models %s\n", args[0])
		fmt.Println("Usage: llamawizard models <list|add|show|remove|delete>")
		os.Exit(1)
	}
}

func runModelsList() {
	st, err := state.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading state: %v\n", err)
		os.Exit(1)
	}

	if len(st.Models) == 0 {
		fmt.Println("No models installed.")
		return
	}

	fmt.Println("Installed models:")
	for i, m := range st.Models {
		sizeStr := ""
		if m.SizeBytes > 0 {
			sizeStr = fmt.Sprintf("  %.1f GB", float64(m.SizeBytes)/(1024*1024*1024))
		}
		fmt.Printf("  %2d. %-30s  %-8s%s\n", i+1, m.Slug, m.Quant, sizeStr)
	}
}

func runModelsShow(name string) {
	st, err := state.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading state: %v\n", err)
		os.Exit(1)
	}

	for _, m := range st.Models {
		if m.Slug == name {
			fmt.Printf("Name:        %s\n", m.Name)
			fmt.Printf("Slug:        %s\n", m.Slug)
			fmt.Printf("Repo:        %s\n", m.HFRepo)
			fmt.Printf("Quant:       %s\n", m.Quant)
			fmt.Printf("File:        %s\n", m.File)
			fmt.Printf("File path:   ~/models/%s/%s\n", m.Slug, m.File)
			if m.SizeBytes > 0 {
				fmt.Printf("Size:        %.1f GB\n", float64(m.SizeBytes)/(1024*1024*1024))
			}
			fmt.Printf("Installed:   %s\n", m.InstalledAt)
			return
		}
	}

	fmt.Printf("Model '%s' not found.\n", name)
	os.Exit(1)
}

func runModelsAdd(args []string) {
	// Just --link without a URL: launch the guided tutorial TUI.
	if len(args) == 1 && args[0] == "--link" {
		if url := runLinkTutorial(); url != "" {
			addModelFromURL(url, "")
		}
		return
	}

	if len(args) >= 2 && args[0] == "--link" {
		addModelFromURL(args[1], extractName(args[2:]))
		return
	}

	st, err := state.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading state: %v\n", err)
		os.Exit(1)
	}

	if st.Port == 0 {
		fmt.Println("Run 'llamawizard' first to set up the wizard.")
		os.Exit(1)
	}

	p := tea.NewProgram(wizard.InitialAddModel(version), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runModelsRemove(name string) {
	st, err := state.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading state: %v\n", err)
		os.Exit(1)
	}

	idx := -1
	for i, m := range st.Models {
		if m.Slug == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		fmt.Printf("Model '%s' not found.\n", name)
		os.Exit(1)
	}

	removed := st.Models[idx]
	st.Models = append(st.Models[:idx], st.Models[idx+1:]...)

	if err := st.Save(""); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving state: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Removed %s from config.\n", removed.Slug)
	fmt.Println("The model file was NOT deleted. Use 'models delete' to also remove the file.")

	regenerateConfig(st)
}

func runModelsDelete(name string, skipConfirm bool) {
	st, err := state.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading state: %v\n", err)
		os.Exit(1)
	}

	idx := -1
	for i, m := range st.Models {
		if m.Slug == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		fmt.Printf("Model '%s' not found.\n", name)
		os.Exit(1)
	}

	removed := st.Models[idx]

	if !skipConfirm {
		fmt.Printf("This will remove '%s' from config AND delete its files.\n", name)
		fmt.Printf("Files in ~/models/%s/ will be deleted.\n", name)
		fmt.Print("Proceed? [y/N] ")
		var answer string
		_, _ = fmt.Scanln(&answer)
		if strings.ToLower(answer) != "y" && strings.ToLower(answer) != "yes" {
			fmt.Println("Cancelled.")
			return
		}
	}

	home, _ := os.UserHomeDir()
	modelDir := filepath.Join(home, "models", removed.Slug)
	if err := os.RemoveAll(modelDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not delete model files: %v\n", err)
	} else {
		fmt.Printf("Deleted %s\n", modelDir)
	}

	st.Models = append(st.Models[:idx], st.Models[idx+1:]...)
	if err := st.Save(""); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving state: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Removed %s from config and disk.\n", removed.Slug)

	regenerateConfig(st)
}

func regenerateConfig(st *state.State) {
	hw, _ := hardware.Detect()
	yamlBytes, err := llamaswap.GenerateConfig(st.Models, st.Port, st.APIKey, st.LlamaCppPath, hw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating config: %v\n", err)
		return
	}
	configPath := state.DefaultConfigPath()
	if err := llamaswap.ForceWrite(configPath, yamlBytes); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
		return
	}
	fmt.Printf("Config updated at %s\n", configPath)
}

func runConfig(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: llamawizard config <show|path>")
		os.Exit(1)
	}

	switch args[0] {
	case "show":
		runConfigShow()
	case "path":
		runConfigPath()
	default:
		fmt.Printf("Unknown subcommand: config %s\n", args[0])
		fmt.Println("Usage: llamawizard config <show|path>")
		os.Exit(1)
	}
}

func runConfigShow() {
	configPath := state.DefaultConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(string(data))
}

func runConfigPath() {
	fmt.Println(state.DefaultConfigPath())
}

func runPi(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: llamawizard pi <install|uninstall>")
		os.Exit(1)
	}

	switch args[0] {
	case "install":
		runPiInstall()
	case "uninstall":
		runPiUninstall()
	default:
		fmt.Printf("Unknown subcommand: pi %s\n", args[0])
		fmt.Println("Usage: llamawizard pi <install|uninstall>")
		os.Exit(1)
	}
}

func runPiInstall() {
	if pi.IsInstalled() {
		fmt.Println("pi is already installed.")
		return
	}

	fmt.Println("Installing pi...")
	if err := pi.Install(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	st, err := state.Load("")
	if err == nil && st.Port != 0 {
		st.PiConfigured = true
		_ = st.Save("")
	}

	fmt.Println("pi installed successfully.")
	fmt.Println("Run 'llamawizard' to configure pi with your local models.")
}

func runPiUninstall() {
	if !pi.IsInstalled() {
		fmt.Println("pi is not installed.")
		return
	}

	fmt.Println("Uninstalling pi...")
	if err := pi.Uninstall(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	st, err := state.Load("")
	if err == nil && st.PiConfigured {
		st.PiConfigured = false
		_ = st.Save("")
	}

	fmt.Println("pi uninstalled successfully.")
}

func printTail(path string, n int) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("  (no log file found)")
		return
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	start := 0
	if len(lines) > n {
		start = len(lines) - n
	}

	for _, line := range lines[start:] {
		fmt.Println("  " + line)
	}
}

func defaultPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchd.PlistName), nil
}
