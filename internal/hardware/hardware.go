package hardware

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// HardwareInfo holds machine-level details for display and build decisions.
type HardwareInfo struct {
	Chip  string // "apple-silicon" or "intel"
	RAM   uint64 // total bytes
	Metal bool   // true on Apple Silicon
	Model string // e.g. "Apple M5 Pro", "Intel(R) Core(TM) i7..."
}

// Detect runs sysctl/uname to populate hardware info. This is deliberately
// thin — whichllm owns the actual model-ranking logic.
func Detect() (HardwareInfo, error) {
	arch, err := archCmd("uname", "-m")
	if err != nil {
		return HardwareInfo{}, fmt.Errorf("detecting architecture: %w", err)
	}

	memsize, err := sysctlUint64("hw.memsize")
	if err != nil {
		return HardwareInfo{}, fmt.Errorf("reading memory size: %w", err)
	}

	var chip string
	var metal bool
	switch arch {
	case "arm64":
		chip = "apple-silicon"
		metal = true
	case "x86_64":
		chip = "intel"
		metal = false
	default:
		return HardwareInfo{}, fmt.Errorf("unknown architecture %q", arch)
	}

	// Best-effort model name; not critical if it fails.
	var model string
	if chip == "apple-silicon" {
		model, _ = sysctlString("machdep.cpu.brand_string")
	} else {
		model, _ = sysctlString("machdep.cpu.brand_string")
	}

	return HardwareInfo{
		Chip:  chip,
		RAM:   memsize,
		Metal: metal,
		Model: model,
	}, nil
}

// archCmd runs a command and returns trimmed stdout.
func archCmd(cmd string, args ...string) (string, error) {
	out, err := exec.Command(cmd, args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// sysctlUint64 reads a sysctl value as uint64.
func sysctlUint64(key string) (uint64, error) {
	out, err := exec.Command("sysctl", "-n", key).Output()
	if err != nil {
		return 0, fmt.Errorf("sysctl -n %s: %w", key, err)
	}
	val, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing sysctl value %q for %s: %w", string(out), key, err)
	}
	return val, nil
}

// sysctlString reads a sysctl value as a trimmed string.
func sysctlString(key string) (string, error) {
	out, err := exec.Command("sysctl", "-n", key).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
