package launchd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDomain(t *testing.T) {
	d := domain()
	if !strings.HasPrefix(d, "gui/") {
		t.Errorf("domain should start with gui/, got %q", d)
	}
}

func TestSpecForLabel(t *testing.T) {
	s := specForLabel("com.test.foo")
	d := domain()
	want := d + "/com.test.foo"
	if s != want {
		t.Errorf("specForLabel = %q, want %q", s, want)
	}
}

func TestWritePlist_CreatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	binaryPath := "/usr/bin/true"
	configPath := filepath.Join(tmpDir, "config.yaml")

	if err := os.WriteFile(configPath, []byte("port: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plistPath := filepath.Join(tmpDir, PlistName)

	logDir, err := defaultLogDir()
	if err != nil {
		t.Fatal(err)
	}
	workDir, err := defaultWorkDir()
	if err != nil {
		t.Fatal(err)
	}

	data := plistData{
		Label:      "com.test.write",
		BinaryPath: binaryPath,
		ConfigPath: configPath,
		Port:       "8080",
		LogDir:     logDir,
		WorkDir:    workDir,
	}

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	f, err := os.Create(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := plistTmpl.Execute(f, data); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatal(err)
	}

	text := string(content)
	checks := []struct{ name, substr string }{
		{"label", "com.test.write"},
		{"binary", binaryPath},
		{"config", configPath},
		{"listen flag", "<string>-listen</string>"},
		{"listen addr", "127.0.0.1:8080"},
		{"RunAtLoad", "<key>RunAtLoad</key>"},
		{"KeepAlive", "<key>KeepAlive</key>"},
		{"xml header", "<?xml version=\"1.0\" encoding=\"UTF-8\"?>"},
		{"doctype", "<!DOCTYPE plist"},
		{"StandardOutPath", "llama-swap.log"},
		{"StandardErrorPath", "llama-swap-error.log"},
	}

	for _, c := range checks {
		if !strings.Contains(text, c.substr) {
			t.Errorf("plist missing %s: expected %q in output", c.name, c.substr)
		}
	}
}

func TestWritePlist_DefaultPath(t *testing.T) {
	tmpDir := t.TempDir()

	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tmpDir)
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })

	binaryPath := "/usr/bin/true"
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("port: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plistPath, err := WritePlist(binaryPath, configPath, 8080)
	if err != nil {
		t.Fatal(err)
	}

	expectedSuffix := filepath.Join("Library", "LaunchAgents", PlistName)
	if !strings.HasSuffix(plistPath, expectedSuffix) {
		t.Errorf("plistPath = %q, want suffix %q", plistPath, expectedSuffix)
	}

	content, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)

	if !strings.Contains(text, binaryPath) {
		t.Error("plist should contain the binary path")
	}
	if !strings.Contains(text, ServiceLabel) {
		t.Error("plist should contain the service label")
	}
}

// renderTestPlist writes a plist rendered from the real template into dir.
func renderTestPlist(t *testing.T, dir, label, port string) string {
	t.Helper()
	plistPath := filepath.Join(dir, label+".plist")
	f, err := os.Create(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	data := plistData{
		Label:      label,
		BinaryPath: "/usr/bin/true",
		ConfigPath: filepath.Join(dir, "config.yaml"),
		Port:       port,
		LogDir:     dir,
		WorkDir:    dir,
	}
	if err := plistTmpl.Execute(f, data); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()
	return plistPath
}

func TestCurrentListenHost(t *testing.T) {
	plistPath := renderTestPlist(t, t.TempDir(), "com.test.listen", "8080")
	h, err := CurrentListenHost(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	if h != "127.0.0.1" {
		t.Errorf("CurrentListenHost = %q, want 127.0.0.1", h)
	}
}

func TestCurrentListenHost_MissingFile(t *testing.T) {
	if _, err := CurrentListenHost("/nonexistent/plist.plist"); err == nil {
		t.Error("CurrentListenHost on a missing file should fail")
	}
}

func TestSetListenHost(t *testing.T) {
	dir := t.TempDir()
	plistPath := renderTestPlist(t, dir, "com.test.listen", "8080")

	// Stub the reload so the test never touches real launchd.
	realInstall := install
	install = func(string) error { return nil }
	t.Cleanup(func() { install = realInstall })

	changed, err := SetListenHost(plistPath, "0.0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("first SetListenHost should report changed")
	}
	text, _ := os.ReadFile(plistPath)
	if !strings.Contains(string(text), "<string>0.0.0.0:8080</string>") {
		t.Errorf("plist should listen on 0.0.0.0:8080, got:\n%s", text)
	}

	// A no-op reload when the host is already set.
	changed, err = SetListenHost(plistPath, "0.0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("SetListenHost on the same host should not report changed")
	}

	// Restore loopback; the port must be preserved.
	changed, err = SetListenHost(plistPath, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("restoring loopback should report changed")
	}
	text, _ = os.ReadFile(plistPath)
	if !strings.Contains(string(text), "<string>127.0.0.1:8080</string>") {
		t.Errorf("plist should listen on 127.0.0.1:8080, got:\n%s", text)
	}
}

func TestSetListenHostFile_NoReload(t *testing.T) {
	dir := t.TempDir()
	plistPath := renderTestPlist(t, dir, "com.test.listenfile", "8080")

	// Stub the reload and count calls: SetListenHostFile must never reload.
	calls := 0
	realInstall := install
	install = func(string) error { calls++; return nil }
	t.Cleanup(func() { install = realInstall })

	changed, err := SetListenHostFile(plistPath, "0.0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("first SetListenHostFile should report changed")
	}
	text, _ := os.ReadFile(plistPath)
	if !strings.Contains(string(text), "<string>0.0.0.0:8080</string>") {
		t.Errorf("plist should listen on 0.0.0.0:8080, got:\n%s", text)
	}

	changed, err = SetListenHostFile(plistPath, "0.0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("SetListenHostFile on the same host should not report changed")
	}

	if calls != 0 {
		t.Errorf("SetListenHostFile must not reload the service, install called %d times", calls)
	}
}

func TestSetListenHost_Malformed(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "bad.plist")
	if err := os.WriteFile(plistPath, []byte("<plist><dict/></plist>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := SetListenHost(plistPath, "0.0.0.0"); err == nil {
		t.Error("SetListenHost on a plist without -listen should fail")
	}
}

func TestUninstall_RemovesPlist(t *testing.T) {
	tmpDir := t.TempDir()
	plistPath := filepath.Join(tmpDir, PlistName)
	if err := os.WriteFile(plistPath, []byte("<plist/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = stopByLabel(ServiceLabel)
	})

	if err := Uninstall(plistPath); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Error("plist should be removed after Uninstall")
	}
}

func TestUninstall_NonexistentIsNoop(t *testing.T) {
	if err := Uninstall("/nonexistent/path/to/plist.plist"); err != nil {
		t.Errorf("Uninstall on nonexistent path should not error: %v", err)
	}
}

func TestStop_NotInstalledIsError(t *testing.T) {
	err := stopByLabel("com.local.llamawizard-test-nonexistent")
	if err == nil {
		t.Error("Stop on nonexistent service should return an error")
	}
}

func TestStatus_NotInstalledIsError(t *testing.T) {
	_, err := statusByLabel("com.local.llamawizard-test-nonexistent")
	if err == nil {
		t.Error("Status on nonexistent service should return an error")
	}
}

func TestLoaded_TracksInstallAndStop(t *testing.T) {
	tmpDir := t.TempDir()
	plistPath := filepath.Join(tmpDir, "test.plist")

	dummyBin := filepath.Join(tmpDir, "dummy.sh")
	if err := os.WriteFile(dummyBin, []byte("#!/bin/sh\nwhile true; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	data := plistData{
		Label:      testLabel,
		BinaryPath: dummyBin,
		ConfigPath: filepath.Join(tmpDir, "config.yaml"),
		Port:       "8080",
		LogDir:     tmpDir,
		WorkDir:    tmpDir,
	}
	f, err := os.Create(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := plistTmpl.Execute(f, data); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()

	t.Cleanup(func() {
		_ = stopByLabel(testLabel)
		_ = os.Remove(plistPath)
	})

	_ = stopByLabel(testLabel) // clean slate
	if loadedByLabel(testLabel) {
		t.Fatal("service should not be loaded before install")
	}

	if err := installWithLabel(plistPath, testLabel); err != nil {
		t.Fatal(err)
	}
	if !loadedByLabel(testLabel) {
		t.Fatal("service should be loaded after install")
	}

	if err := stopByLabel(testLabel); err != nil {
		t.Fatal(err)
	}
	if loadedByLabel(testLabel) {
		t.Fatal("service should not be loaded after stop")
	}
}

const testLabel = "com.local.llamawizard-test"

func TestLifecycle_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	plistPath := filepath.Join(tmpDir, "test.plist")

	dummyBin := filepath.Join(tmpDir, "dummy.sh")
	if err := os.WriteFile(dummyBin, []byte("#!/bin/sh\nwhile true; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("port: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	logDir := filepath.Join(tmpDir, "logs")
	workDir := tmpDir

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}

	data := plistData{
		Label:      testLabel,
		BinaryPath: dummyBin,
		ConfigPath: configPath,
		Port:       "8080",
		LogDir:     logDir,
		WorkDir:    workDir,
	}

	f, err := os.Create(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := plistTmpl.Execute(f, data); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()

	t.Cleanup(func() {
		_ = stopByLabel(testLabel)
		_ = os.Remove(plistPath)
	})

	t.Run("install", func(t *testing.T) {
		if err := installWithLabel(plistPath, testLabel); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("status-after-install", func(t *testing.T) {
		out, err := statusByLabel(testLabel)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("status after install:\n%s", out)
		if out == "" {
			t.Error("status output should not be empty")
		}
	})

	t.Run("state-is-running", func(t *testing.T) {
		out, _ := statusByLabel(testLabel)
		if out == "" || !strings.Contains(out, "running") {
			t.Skip("state may use different terminology across macOS versions")
		}
	})

	t.Run("start-on-already-running", func(t *testing.T) {
		if err := startOrBootstrap(plistPath, testLabel); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("stop", func(t *testing.T) {
		if err := stopByLabel(testLabel); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("status-after-stop", func(t *testing.T) {
		_, err := statusByLabel(testLabel)
		if err == nil {
			t.Error("status after stop should return an error for an unloaded service")
		}
	})

	t.Run("restart-after-stop", func(t *testing.T) {
		if err := startOrBootstrap(plistPath, testLabel); err != nil {
			t.Fatal(err)
		}

		out, err := statusByLabel(testLabel)
		if err != nil {
			t.Fatal(err)
		}
		if out == "" {
			t.Error("status after restart should not be empty")
		}
	})

	t.Run("final-stop", func(t *testing.T) {
		if err := stopByLabel(testLabel); err != nil {
			t.Fatal(err)
		}
	})
}
