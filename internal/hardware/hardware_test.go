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

func TestVMStatAvailablePages(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		want    uint64
		wantErr bool
	}{
		{
			name: "macOS 27 sample",
			out: `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                                     8831.
Pages active:                                 134882.
Pages inactive:                               131998.
Pages speculative:                              1894.
Pages throttled:                                   0.
Pages wired down:                            2482253.
Pages purgeable:                                   2.
"Translation faults":                     2669730600.
Pages copy-on-write:                           201141540.
`,
			want: 142725, // 8831 + 131998 + 1894 + 2
		},
		{
			name: "minimal older format",
			out:  "Mach Virtual Memory Statistics: (page size of 4096 bytes)\nPages free:\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t1000.\nPages active:\t\t100.\nPages inactive:\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t500.\n",
			want: 1500, // 1000 + 500; speculative/purgeable absent
		},
		{
			name:    "missing Pages inactive",
			out:     "Pages free:\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t1000.\n",
			wantErr: true,
		},
		{
			name:    "missing Pages free",
			out:     "Pages inactive:\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t\t500.\n",
			wantErr: true,
		},
		{
			name:    "empty output",
			out:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := vmStatAvailablePages(tt.out)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("vmStatAvailablePages = %d, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("vmStatAvailablePages failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("vmStatAvailablePages = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAvailableRAM_Range(t *testing.T) {
	avail, err := AvailableRAM()
	if err != nil {
		t.Fatalf("AvailableRAM failed: %v", err)
	}
	total, err := TotalRAM()
	if err != nil {
		t.Fatalf("TotalRAM failed: %v", err)
	}
	if avail == 0 {
		t.Fatal("AvailableRAM should be > 0")
	}
	if avail > total {
		t.Errorf("AvailableRAM %d exceeds TotalRAM %d", avail, total)
	}
	t.Logf("AvailableRAM: %d bytes (%.1f GiB) of %d bytes (%.1f GiB)",
		avail, float64(avail)/(1<<30), total, float64(total)/(1<<30))
}
