package launchd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"text/template"
)

// ServiceLabel is the launchd service identifier.
const ServiceLabel = "com.local.llama-swap"

// PlistName is the plist filename.
const PlistName = "com.local.llama-swap.plist"

var plistTmpl = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{.Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.BinaryPath}}</string>
		<string>-config</string>
		<string>{{.ConfigPath}}</string>
		<string>-listen</string>
		<string>127.0.0.1:{{.Port}}</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>StandardOutPath</key>
	<string>{{.LogDir}}/llama-swap.log</string>
	<key>StandardErrorPath</key>
	<string>{{.LogDir}}/llama-swap-error.log</string>
	<key>WorkingDirectory</key>
	<string>{{.WorkDir}}</string>
</dict>
</plist>
`))

type plistData struct {
	Label      string
	BinaryPath string
	ConfigPath string
	Port       string
	LogDir     string
	WorkDir    string
}

func domainFor(uid int) string {
	return fmt.Sprintf("gui/%d", uid)
}

func domain() string {
	return domainFor(os.Getuid())
}

func specForLabelWithUID(label string, uid int) string {
	return domainFor(uid) + "/" + label
}

func specForLabel(label string) string {
	return specForLabelWithUID(label, os.Getuid())
}

func defaultPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", PlistName), nil
}

func defaultLogDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "ai", "logs"), nil
}

func defaultWorkDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "ai"), nil
}

// WritePlist generates a LaunchAgent plist and writes it to disk.
//
// The plist is written to ~/Library/LaunchAgents/com.local.llama-swap.plist.
// Parent directories are created if needed. Returns the path to the written file.
func WritePlist(binaryPath, configPath string, port int) (string, error) {
	plistPath, err := defaultPlistPath()
	if err != nil {
		return "", fmt.Errorf("plist path: %w", err)
	}

	logDir, err := defaultLogDir()
	if err != nil {
		return "", fmt.Errorf("log dir: %w", err)
	}

	workDir, err := defaultWorkDir()
	if err != nil {
		return "", fmt.Errorf("work dir: %w", err)
	}

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", fmt.Errorf("creating log dir: %w", err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", fmt.Errorf("creating work dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return "", fmt.Errorf("creating LaunchAgents dir: %w", err)
	}

	data := plistData{
		Label:      ServiceLabel,
		BinaryPath: binaryPath,
		ConfigPath: configPath,
		Port:       fmt.Sprintf("%d", port),
		LogDir:     logDir,
		WorkDir:    workDir,
	}

	f, err := os.Create(plistPath)
	if err != nil {
		return "", fmt.Errorf("creating plist: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err := plistTmpl.Execute(f, data); err != nil {
		return "", fmt.Errorf("rendering plist: %w", err)
	}

	return plistPath, nil
}

// Install bootstraps the LaunchAgent so it loads at login and runs now.
//
// It calls launchctl bootstrap to load the service immediately, then
// launchctl enable to ensure it starts on next login.
func Install(plistPath string) error {
	return installWithLabel(plistPath, ServiceLabel)
}

func installWithLabel(plistPath, label string) error {
	spec := specForLabel(label)
	d := domain()

	bootoutCmd := exec.Command("launchctl", "bootout", spec)
	_ = bootoutCmd.Run()

	bootstrapCmd := exec.Command("launchctl", "bootstrap", d, plistPath)
	if out, err := bootstrapCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bootstrap: %w\n%s", err, string(out))
	}

	enableCmd := exec.Command("launchctl", "enable", spec)
	_ = enableCmd.Run()

	return nil
}

// Uninstall removes the LaunchAgent from launchd and deletes the plist file.
func Uninstall(plistPath string) error {
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		return nil
	}

	spec := specForLabel(ServiceLabel)

	bootoutCmd := exec.Command("launchctl", "bootout", spec)
	_ = bootoutCmd.Run()

	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing plist: %w", err)
	}

	return nil
}

// Start ensures the service is bootstrapped and running in the current
// user's gui domain.
//
// It first attempts a kickstart (which only works on already-bootstrapped
// services). If that fails, it bootstraps the service to load it fresh.
func Start(plistPath string) error {
	return StartUID(os.Getuid(), plistPath)
}

// StartUID is Start for the gui domain of uid. Root can kickstart and
// bootstrap any gui domain; a non-root user can only reach its own.
func StartUID(uid int, plistPath string) error {
	return startOrBootstrapWithUID(plistPath, ServiceLabel, uid)
}

func startOrBootstrapWithUID(plistPath, label string, uid int) error {
	spec := specForLabelWithUID(label, uid)
	d := domainFor(uid)

	kickCmd := exec.Command("launchctl", "kickstart", spec)
	if _, err := kickCmd.CombinedOutput(); err == nil {
		return nil
	}

	bootstrapCmd := exec.Command("launchctl", "bootstrap", d, plistPath)
	if out, err := bootstrapCmd.CombinedOutput(); err != nil {
		kickCmd2 := exec.Command("launchctl", "kickstart", spec)
		if _, err2 := kickCmd2.CombinedOutput(); err2 == nil {
			return nil
		}
		return fmt.Errorf("bootstrap: %w\n%s", err, string(out))
	}

	return nil
}

// Stop unloads the service from launchd.
//
// bootout is used instead of kill because the plist includes KeepAlive,
// which would cause launchd to immediately restart a killed process.
func Stop() error {
	return stopByLabel(ServiceLabel)
}

func stopByLabel(label string) error {
	spec := specForLabel(label)

	cmd := exec.Command("launchctl", "bootout", spec)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bootout: %w\n%s", err, string(out))
	}

	return nil
}

// Status returns the output of launchctl print for the service in the
// current user's gui domain.
func Status() (string, error) {
	return StatusUID(os.Getuid())
}

// StatusUID is Status for the gui domain of uid. Root can print any gui
// domain; a non-root user can only reach its own.
func StatusUID(uid int) (string, error) {
	return statusByLabelWithUID(ServiceLabel, uid)
}

func statusByLabelWithUID(label string, uid int) (string, error) {
	spec := specForLabelWithUID(label, uid)

	out, err := exec.Command("launchctl", "print", spec).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("print: %w\n%s", err, string(out))
	}

	return string(out), nil
}

// TargetUser returns the user whose llama-swap service this invocation
// should manage.
//
// A non-root caller manages its own service. Root (e.g. via sudo) manages
// the service of the user who invoked sudo, resolved from SUDO_USER; if
// SUDO_USER is unset there is no target user to watch.
func TargetUser() (*user.User, error) {
	if os.Geteuid() != 0 {
		return user.Current()
	}
	name := os.Getenv("SUDO_USER")
	if name == "" {
		return nil, errors.New("running as root but SUDO_USER is not set — run without sudo")
	}
	u, err := user.Lookup(name)
	if err != nil {
		return nil, fmt.Errorf("looking up SUDO_USER %q: %w", name, err)
	}
	return u, nil
}
