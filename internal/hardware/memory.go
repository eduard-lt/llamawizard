package hardware

import "fmt"

const (
	// MinReservedRAM is the minimum RAM (in bytes) reserved for the OS and
	// other applications. This ensures we never starve the rest of the system.
	MinReservedRAM uint64 = 8 * 1024 * 1024 * 1024 // 8 GB

	// ReservedRatio is the fraction of total RAM reserved as a safety margin
	// when that amount exceeds MinReservedRAM.
	ReservedRatio = 0.15
)

// AvailableRAM estimates how much RAM is currently usable for model weights
// and KV cache. It reads vm.page_free_count and vm.page_inactive_count via
// sysctl and returns the sum (in bytes).
//
// On macOS, "inactive" pages are clean pages that can be reclaimed by the
// kernel without swapping — they are effectively available.
func AvailableRAM() (uint64, error) {
	pageSize, err := sysctlUint64("hw.pagesize")
	if err != nil {
		return 0, fmt.Errorf("reading page size: %w", err)
	}

	freePages, err := sysctlUint64("vm.page_free_count")
	if err != nil {
		return 0, fmt.Errorf("reading free pages: %w", err)
	}

	inactivePages, err := sysctlUint64("vm.page_inactive_count")
	if err != nil {
		return 0, fmt.Errorf("reading inactive pages: %w", err)
	}

	return (freePages + inactivePages) * pageSize, nil
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
