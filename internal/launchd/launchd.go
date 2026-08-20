package launchd

import (
	"fmt"
	"os"
	"os/exec"
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

func domain() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func specForLabel(label string) string {
	return domain() + "/" + label
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

// Start ensures the service is bootstrapped and running.
//
// It first attempts a kickstart (which only works on already-bootstrapped
// services). If that fails, it bootstraps the service to load it fresh.
func Start(plistPath string) error {
	return startOrBootstrap(plistPath, ServiceLabel)
}

func startOrBootstrap(plistPath, label string) error {
	spec := specForLabel(label)
	d := domain()

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

// Status returns the output of launchctl print for the service.
func Status() (string, error) {
	return statusByLabel(ServiceLabel)
}

func statusByLabel(label string) (string, error) {
	spec := specForLabel(label)

	out, err := exec.Command("launchctl", "print", spec).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("print: %w\n%s", err, string(out))
	}

	return string(out), nil
}
