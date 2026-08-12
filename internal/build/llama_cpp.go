package build

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	llamaCppRepo     = "https://github.com/ggml-org/llama.cpp"
	llamaCppCloneDir = "llama.cpp"
)

// EnsureLlamaCpp returns the path to a working llama-server binary,
// using the three-tier fallback from the spec:
//
//  1. Check for an existing binary at well-known paths, verify with --version.
//  2. Try brew install llama.cpp, then locate the binary.
//  3. Source build: git clone cmake build with chip-appropriate flags.
//
// The chip parameter is "apple-silicon" or "intel". On Apple Silicon
// the cmake build adds -DGGML_METAL=ON.
//
// Source builds happen in ~/.local/ai/llama.cpp. If that directory already
// exists but the binary is missing, it is treated as a rebuild case (not
// an error).
func EnsureLlamaCpp(chip string) (string, error) {
	if path, ok := tryExistingBinary(); ok {
		return path, nil
	}

	if path, ok := tryBrewInstall(); ok {
		return path, nil
	}

	return trySourceBuild(chip)
}

// tryExistingBinary checks common paths for a pre-existing llama-server
// and verifies it runs --version successfully.
func tryExistingBinary() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}

	candidates := []string{
		filepath.Join(home, ".local", "ai", "llama.cpp", "build", "bin", "llama-server"),
	}

	if p, err := exec.LookPath("llama-server"); err == nil {
		candidates = append(candidates, p)
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		cmd := exec.Command(path, "--version")
		out, err := cmd.CombinedOutput()
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(out)) != "" {
			return path, true
		}
	}

	return "", false
}

// tryBrewInstall attempts to install llama.cpp via Homebrew and returns
// the binary path on success.
func tryBrewInstall() (string, bool) {
	if _, err := exec.LookPath("brew"); err != nil {
		return "", false
	}

	installCmd := exec.Command("brew", "install", "llama.cpp")
	var stderr bytes.Buffer
	installCmd.Stdout = io.Discard
	installCmd.Stderr = &stderr
	if err := installCmd.Run(); err != nil {
		return "", false
	}

	p, err := exec.LookPath("llama-server")
	if err != nil {
		return "", false
	}

	cmd := exec.Command(p, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "", false
	}

	return p, true
}

// trySourceBuild clones (or reuses) the llama.cpp repo and builds it from
// source with chip-appropriate cmake flags.
//
// The build directory is ~/.local/ai/llama.cpp.
func trySourceBuild(chip string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	baseDir := filepath.Join(home, ".local", "ai")
	repoDir := filepath.Join(baseDir, llamaCppCloneDir)
	buildDir := filepath.Join(repoDir, "build")
	binary := filepath.Join(buildDir, "bin", "llama-server")

	if _, err := os.Stat(binary); err == nil {
		cmd := exec.Command(binary, "--version")
		out, err := cmd.CombinedOutput()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return binary, nil
		}
	}

	hasBuildDir := false
	if _, err := os.Stat(buildDir); err == nil {
		hasBuildDir = true
	}

	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", fmt.Errorf("creating ~/.local/ai: %w", err)
	}

	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		cloneCmd := exec.Command("git", "clone", "--depth", "1", llamaCppRepo, repoDir)
		var cloneStderr bytes.Buffer
		cloneCmd.Stdout = io.Discard
		cloneCmd.Stderr = &cloneStderr
		if err := cloneCmd.Run(); err != nil {
			return "", fmt.Errorf("git clone: %w\n%s", err, strings.TrimSpace(cloneStderr.String()))
		}
	}

	if !hasBuildDir {
		if err := os.MkdirAll(buildDir, 0o755); err != nil {
			return "", fmt.Errorf("creating build directory: %w", err)
		}
	}

	cmakeArgs := []string{"-B", "build", "-DCMAKE_BUILD_TYPE=Release"}
	if chip == "apple-silicon" {
		cmakeArgs = append(cmakeArgs, "-DGGML_METAL=ON")
	}

	cmakeCmd := exec.Command("cmake", cmakeArgs...)
	cmakeCmd.Dir = repoDir
	var cmakeStderr bytes.Buffer
	cmakeCmd.Stdout = io.Discard
	cmakeCmd.Stderr = &cmakeStderr
	if err := cmakeCmd.Run(); err != nil {
		return "", fmt.Errorf("cmake configure: %w\n%s", err, strings.TrimSpace(cmakeStderr.String()))
	}

	nproc, err := nproc()
	if err != nil {
		nproc = 4
	}

	buildArgs := []string{"--build", "build", "--config", "Release", "-j", fmt.Sprintf("%d", nproc)}
	buildCmd := exec.Command("cmake", buildArgs...)
	buildCmd.Dir = repoDir
	var buildStderr bytes.Buffer
	buildCmd.Stdout = io.Discard
	buildCmd.Stderr = &buildStderr
	if err := buildCmd.Run(); err != nil {
		return "", fmt.Errorf("cmake build: %w\n%s", err, strings.TrimSpace(buildStderr.String()))
	}

	if _, err := os.Stat(binary); err != nil {
		return "", fmt.Errorf("build completed but binary not found at %s", binary)
	}

	cmd := exec.Command(binary, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "", fmt.Errorf("binary at %s does not produce expected --version output", binary)
	}

	return binary, nil
}

// nproc returns the number of hardware CPUs on macOS.
func nproc() (int, error) {
	out, err := exec.Command("sysctl", "-n", "hw.ncpu").Output()
	if err != nil {
		return 0, err
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &n); err != nil {
		return 0, err
	}
	if n < 1 {
		n = 1
	}
	return n, nil
}
