package hardware

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const (
	// MinReservedRAM is the minimum RAM (in bytes) reserved for the OS and
	// other applications. This ensures we never starve the rest of the system.
	MinReservedRAM uint64 = 8 * 1024 * 1024 * 1024 // 8 GB

	// ReservedRatio is the fraction of total RAM reserved as a safety margin
	// when that amount exceeds MinReservedRAM.
	ReservedRatio = 0.15
)

// AvailableRAM estimates how much RAM is currently usable for model weights
// and KV cache. It parses vm_stat and returns the sum of free + inactive
// pages (plus speculative and purgeable pages on newer macOS), in bytes.
//
// vm_stat is used instead of the vm.page_free_count /
// vm.page_inactive_count sysctls because vm.page_inactive_count was removed
// in macOS 27, while vm_stat still reports all counters on every macOS
// version.
//
// On macOS, "inactive" pages are clean pages that can be reclaimed by the
// kernel without swapping — they are effectively available.
func AvailableRAM() (uint64, error) {
	pageSize, err := sysctlUint64("hw.pagesize")
	if err != nil {
		return 0, fmt.Errorf("reading page size: %w", err)
	}

	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0, fmt.Errorf("running vm_stat: %w", err)
	}

	pages, err := vmStatAvailablePages(string(out))
	if err != nil {
		return 0, fmt.Errorf("parsing vm_stat output: %w", err)
	}

	return pages * pageSize, nil
}

// vmStatAvailablePages parses vm_stat output and returns the number of
// available pages: free + inactive, plus speculative and purgeable when
// present (newer macOS). The "Pages free" and "Pages inactive" lines are
// required; their absence is an error to guard against format changes.
func vmStatAvailablePages(out string) (uint64, error) {
	var free, inactive, speculative, purgeable uint64
	haveFree, haveInactive := false, false

	for _, line := range strings.Split(out, "\n") {
		var n uint64
		var err error
		switch {
		case strings.HasPrefix(line, "Pages free:"):
			n, err = trailingNumber(line)
			if err != nil {
				return 0, err
			}
			free, haveFree = n, true
		case strings.HasPrefix(line, "Pages inactive:"):
			n, err = trailingNumber(line)
			if err != nil {
				return 0, err
			}
			inactive, haveInactive = n, true
		case strings.HasPrefix(line, "Pages speculative:"):
			n, err = trailingNumber(line)
			if err != nil {
				return 0, err
			}
			speculative = n
		case strings.HasPrefix(line, "Pages purgeable:"):
			n, err = trailingNumber(line)
			if err != nil {
				return 0, err
			}
			purgeable = n
		}
	}

	if !haveFree {
		return 0, fmt.Errorf("vm_stat output missing \"Pages free\" line")
	}
	if !haveInactive {
		return 0, fmt.Errorf("vm_stat output missing \"Pages inactive\" line")
	}

	return free + inactive + speculative + purgeable, nil
}

// trailingNumber parses the right-aligned number at the end of a vm_stat
// line, e.g. "Pages free: ... 8831." → 8831. vm_stat pads values with
// spaces and terminates them with a dot, so we strip the trailing dot and
// whitespace, then parse the trailing run of digits.
func trailingNumber(line string) (uint64, error) {
	s := strings.TrimRight(line, " \t")
	s = strings.TrimSuffix(s, ".")
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	digits := s[i:]
	if digits == "" {
		return 0, fmt.Errorf("no number found in vm_stat line %q", line)
	}
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing number in vm_stat line %q: %w", line, err)
	}
	return n, nil
}

// UsableRAMBudget returns a conservative estimate of how much RAM can be
// safely used for model weights + KV cache, accounting for OS overhead.
//
// It reserves the larger of MinReservedRAM (8 GB) or ReservedRatio (15%) of
// total RAM. Returns 0 if the safety margin exceeds total RAM.
func UsableRAMBudget(totalRAM uint64) uint64 {
	reserved := MinReservedRAM
	if ratioReserved := uint64(float64(totalRAM) * ReservedRatio); ratioReserved > reserved {
		reserved = ratioReserved
	}

	if reserved >= totalRAM {
		return 0
	}
	return totalRAM - reserved
}

// DetectAndMemory performs hardware detection and additionally queries
// currently available memory via sysctl.
func DetectAndMemory() (HardwareInfo, uint64, error) {
	hw, err := Detect()
	if err != nil {
		return HardwareInfo{}, 0, err
	}

	avail := uint64(0)
	if ram, err := AvailableRAM(); err == nil {
		avail = ram
	}

	return hw, avail, nil
}
