package build

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// DepStatus reports whether a single build dependency is available.
type DepStatus struct {
	Name    string // human-readable name, e.g. "cmake"
	Present bool   // true if the tool was found
	Path    string // resolved path (empty when not present)
}

// CheckDeps probes for all required build dependencies and returns their status.
func CheckDeps() []DepStatus {
	return []DepStatus{
		checkTool("git"),
		checkHomebrew(),
		checkXcodeCLT(),
		checkTool("cmake"),
		checkTool("uv"),
	}
}

// Missing returns only the dependencies that are not present.
func (s DepStatus) Missing() bool {
	return !s.Present
}

// checkTool looks up a command on PATH via "which".
func checkTool(name string) DepStatus {
	path, err := exec.LookPath(name)
	return DepStatus{
		Name:    name,
		Present: err == nil,
		Path:    path,
	}
}

// checkHomebrew verifies Homebrew is installed and reachable.
// Checks PATH first, then known install directories (brew may have been
// installed in another terminal without the current shell reloading PATH).
func checkHomebrew() DepStatus {
	path, err := exec.LookPath("brew")
	if err != nil {
		for _, p := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
			if _, stErr := os.Stat(p); stErr == nil {
				path = p
				break
			}
		}
	}
	if path == "" {
		return DepStatus{Name: "Homebrew", Present: false}
	}

	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return DepStatus{Name: "Homebrew", Present: false}
	}

	_ = out
	return DepStatus{Name: "Homebrew", Present: true, Path: path}
}

// checkXcodeCLT uses "xcode-select -p" to detect Xcode Command Line Tools.
func checkXcodeCLT() DepStatus {
	out, err := exec.Command("xcode-select", "-p").Output()
	if err != nil {
		return DepStatus{Name: "Xcode CLT", Present: false}
	}
	return DepStatus{
		Name:    "Xcode CLT",
		Present: true,
		Path:    strings.TrimSpace(string(out)),
	}
}

// Summary prints a human-readable one-line status for each dependency.
func Summary(statuses []DepStatus) string {
	var parts []string
	for _, s := range statuses {
		icon := "✅"
		if !s.Present {
			icon = "❌"
		}
		parts = append(parts, fmt.Sprintf("%s %s", icon, s.Name))
	}
	return strings.Join(parts, "  ")
}

// AllPresent returns true only if every dependency is found.
func AllPresent(statuses []DepStatus) bool {
	for _, s := range statuses {
		if !s.Present {
			return false
		}
	}
	return true
}

// BrewPresent returns the resolved brew path if Homebrew is available.
func BrewPresent(statuses []DepStatus) (string, bool) {
	for _, s := range statuses {
		if s.Name == "Homebrew" && s.Present {
			return s.Path, true
		}
	}
	return "", false
}

// NeedsManualInstall returns true if any non-brew-installable dep is missing.
// That is: Homebrew itself or Xcode CLT. These need user intervention.
func NeedsManualInstall(statuses []DepStatus) bool {
	for _, s := range statuses {
		if s.Missing() && (s.Name == "Homebrew" || s.Name == "Xcode CLT") {
			return true
		}
	}
	return false
}

// NeedsBrewInstall returns the subset of missing deps that brew can install.
func NeedsBrewInstall(statuses []DepStatus) (missing []DepStatus) {
	for _, s := range statuses {
		if s.Missing() && s.Name != "Homebrew" && s.Name != "Xcode CLT" {
			missing = append(missing, s)
		}
	}
	return missing
}

// HomebrewInstallURL is the canonical URL Homebrew points users to.
const HomebrewInstallURL = "https://brew.sh"

// InstallMissing installs any brew-manageable dependencies that are missing.
// Homebrew itself and Xcode CLT cannot be installed via brew, so they are
// handled separately with custom error returns.
func InstallMissing(statuses []DepStatus) error {
	var missing []DepStatus
	for _, s := range statuses {
		if s.Missing() {
			missing = append(missing, s)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	for _, s := range missing {
		if s.Name == "Homebrew" {
			return ErrHomebrewMissing{}
		}
	}

	for _, s := range missing {
		if s.Name == "Xcode CLT" {
			return ErrXcodeCLTMissing{}
		}
	}

	var pkgs []string
	for _, s := range missing {
		pkgs = append(pkgs, s.Name)
	}

	fmt.Fprintf(os.Stderr, "Installing missing dependencies: %s\n", strings.Join(pkgs, ", "))
	cmd := exec.Command("brew", append([]string{"install"}, pkgs...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ErrHomebrewMissing is returned when Homebrew itself is not installed.
type ErrHomebrewMissing struct{}

func (ErrHomebrewMissing) Error() string {
	return "homebrew is not installed — see https://brew.sh for installation instructions"
}

// ErrXcodeCLTMissing is returned when Xcode CLT is not installed.
type ErrXcodeCLTMissing struct{}

func (ErrXcodeCLTMissing) Error() string {
	return "xcode command line tools are not installed — run: xcode-select --install"
}

// AutoInstallBrew installs the given deps via the resolved brew binary path.
func AutoInstallBrew(brewPath string, installable []DepStatus) error {
	if len(installable) == 0 {
		return nil
	}
	var pkgs []string
	for _, s := range installable {
		pkgs = append(pkgs, s.Name)
	}
	cmd := exec.Command(brewPath, append([]string{"install"}, pkgs...)...)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("brew install %s:\n%s", strings.Join(pkgs, " "), strings.TrimSpace(stderr.String()))
	}
	return nil
}

// InstallCommand returns the terminal command to install a dependency.
func InstallCommand(name string) string {
	switch name {
	case "Homebrew":
		return `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`
	case "Xcode CLT":
		return "xcode-select --install"
	default:
		return "brew install " + name
	}
}
