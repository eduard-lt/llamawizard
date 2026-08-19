package hardware

import (
	"testing"
)

func TestDetect(t *testing.T) {
	h, err := Detect()
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if h.Chip == "" {
		t.Fatal("Chip should not be empty")
	}
	if h.Chip != "apple-silicon" && h.Chip != "intel" {
		t.Errorf("unexpected chip family: %q", h.Chip)
	}

	if h.RAM == 0 {
		t.Fatal("RAM should not be 0")
	}
	gb := h.RAM / (1024 * 1024 * 1024)
	t.Logf("Chip: %s, RAM: %d GB (%d bytes), Metal: %v", h.Chip, gb, h.RAM, h.Metal)

	// Metal should match chip family.
	if h.Chip == "apple-silicon" && !h.Metal {
		t.Error("Apple Silicon should have Metal = true")
	}
	if h.Chip == "intel" && h.Metal {
		t.Error("Intel should have Metal = false")
	}

	if h.RAM < 4*(1024*1024*1024) {
		t.Errorf("RAM %d bytes seems too low for a modern Mac", h.RAM)
	}
}

func TestTotalRAM(t *testing.T) {
	ram, err := TotalRAM()
	if err != nil {
		t.Fatalf("TotalRAM failed: %v", err)
	}
	if ram == 0 {
		t.Fatal("TotalRAM should be > 0")
	}
	t.Logf("TotalRAM: %d bytes (%d GB)", ram, ram/(1024*1024*1024))
}
