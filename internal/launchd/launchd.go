package launchd

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// install loads a plist into launchd. Indirection so tests can skip the
// real launchctl.
var install = Install

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

// CurrentListenHost returns the host part of the plist's -listen address
// (e.g. "127.0.0.1" or "0.0.0.0").
func CurrentListenHost(plistPath string) (string, error) {
	data, err := os.ReadFile(plistPath)
	if err != nil {
		return "", fmt.Errorf("reading plist: %w", err)
	}
	addr, err := listenAddrIn(string(data))
	if err != nil {
		return "", err
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("parsing listen address %q: %w", addr, err)
	}
	return host, nil
}

// SetListenHost rewrites the host of the plist's -listen address (the port
// is preserved) and reloads the service so the new address takes effect.
//
// It returns changed=false without touching the service when the plist
// already listens on host. The reload is a bootout+bootstrap cycle, so the
// service briefly restarts.
func SetListenHost(plistPath, host string) (bool, error) {
	changed, err := SetListenHostFile(plistPath, host)
	if err != nil || !changed {
		return changed, err
	}
	if err := install(plistPath); err != nil {
		return false, fmt.Errorf("plist updated but service reload failed: %w", err)
	}
	return true, nil
}

// SetListenHostFile rewrites the host of the plist's -listen address (the
// port is preserved) without reloading the service. The new address takes
// effect the next time the service is loaded.
//
// It returns changed=false without touching the file when the plist already
// listens on host.
func SetListenHostFile(plistPath, host string) (bool, error) {
	current, err := CurrentListenHost(plistPath)
	if err != nil {
		return false, err
	}
	if current == host {
		return false, nil
	}
	if err := setListenHostFile(plistPath, host); err != nil {
		return false, err
	}
	return true, nil
}

// Loaded reports whether the service is currently loaded in the user's gui
// domain — running, exited, or waiting. A service that was stopped with
// bootout (e.g. 'llamawizard stop') is not loaded.
func Loaded() bool {
	return loadedByLabel(ServiceLabel)
}

func loadedByLabel(label string) bool {
	_, err := statusByLabel(label)
	return err == nil
}

// listenAddrIn returns the address string that follows the -listen flag in a
// rendered plist.
func listenAddrIn(plist string) (string, error) {
	lines := strings.Split(plist, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "<string>-listen</string>" {
			continue
		}
		for _, next := range lines[i+1:] {
			next = strings.TrimSpace(next)
			if next == "" {
				continue
			}
			if strings.HasPrefix(next, "<string>") && strings.HasSuffix(next, "</string>") {
				return next[len("<string>") : len(next)-len("</string>")], nil
			}
			return "", fmt.Errorf("malformed plist: expected an address after -listen, got %q", next)
		}
		return "", fmt.Errorf("malformed plist: -listen has no value")
	}
	return "", fmt.Errorf("plist has no -listen argument")
}

// setListenHostFile rewrites only the -listen address line in the plist
// file, preserving the port and every other byte.
func setListenHostFile(plistPath, host string) error {
	data, err := os.ReadFile(plistPath)
	if err != nil {
		return fmt.Errorf("reading plist: %w", err)
	}
	old, err := listenAddrIn(string(data))
	if err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(old)
	if err != nil {
		return fmt.Errorf("parsing listen address %q: %w", old, err)
	}
	out := strings.Replace(string(data), "<string>"+old+"</string>", "<string>"+host+":"+port+"</string>", 1)
	if out == string(data) {
		return fmt.Errorf("plist does not contain listen address %q", old)
	}
	if err := os.WriteFile(plistPath, []byte(out), 0o644); err != nil {
		return fmt.Errorf("writing plist: %w", err)
	}
	return nil
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
