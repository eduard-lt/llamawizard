package ctxcalc

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eduard-lt/llamawizard/internal/gguf"
	"github.com/eduard-lt/llamawizard/internal/hardware"
)

func TestCalcSingle_RealQwenModel(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}

	modelPath := filepath.Join(home, "models/qwen3.6-27b/Qwen-Qwen3.6-27B-Q4_K_M.gguf")
	st, err := os.Stat(modelPath)
	if err != nil {
		t.Skipf("model not found: %v", err)
	}

	hw, err := hardware.Detect()
	if err != nil {
		t.Skipf("hardware detect failed: %v", err)
	}

	meta, err := gguf.ReadMetadata(modelPath)
	if err != nil {
		t.Fatalf("GGUF parse failed: %v", err)
	}

	bpt := meta.KVCacheBytesPerToken(true)
	usableRAM := hardware.UsableRAMBudget(hw.RAM)
	availableForKV := usableRAM - uint64(st.Size())
	maxByRAM := availableForKV / bpt

	t.Logf("Qwen3.6 27B Q4_K_M")
	t.Logf("  File: %d GB, RAM: %d GB, Usable: %d GB, Avail for KV: %d GB",
		st.Size()/(1024*1024*1024), hw.RAM/(1024*1024*1024),
		usableRAM/(1024*1024*1024), availableForKV/(1024*1024*1024))
	t.Logf("  arch=%s, n_ctx_train=%d, n_layers=%d, n_heads=%d, n_kv_heads=%d, head_dim=%d",
		meta.Arch, meta.NCtxTrain, meta.NBlock, meta.NHeads, meta.KVHeads(), meta.HeadDim())
	t.Logf("  KV bpt (f16): %d bytes, Kv bpt (q8_0): %d bytes", bpt, meta.KVCacheBytesPerToken(false))
	t.Logf("  Max ctx by RAM (f16): %d, (q8_0): %d", maxByRAM, availableForKV/meta.KVCacheBytesPerToken(false))

	calc := New(hw)
	r := calc.CalcSingle(modelPath, st.Size(), true)
	t.Logf("  SMART ctx (f16): %d (used_default=%v)", r.CtxSize, r.UsedDefault)

	rQ8 := calc.CalcSingle(modelPath, st.Size(), false)
	t.Logf("  SMART ctx (q8_0): %d", rQ8.CtxSize)

	if r.UsedDefault {
		t.Error("smart calc should not fall back to default for a valid GGUF file")
	}
}

func TestCalcSingle_RealGemmaModel(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}

	modelPath := filepath.Join(home, "models/gemma-4-26b-a4b-it/gemma-4-26B-A4B-it-UD-Q6_K_XL.gguf")
	st, err := os.Stat(modelPath)
	if err != nil {
		t.Skipf("model not found: %v", err)
	}

	hw, err := hardware.Detect()
	if err != nil {
		t.Skipf("hardware detect failed: %v", err)
	}

	meta, err := gguf.ReadMetadata(modelPath)
	if err != nil {
		t.Fatalf("GGUF parse failed: %v", err)
	}

	bpt := meta.KVCacheBytesPerToken(true)
	usableRAM := hardware.UsableRAMBudget(hw.RAM)
	availableForKV := usableRAM - uint64(st.Size())
	maxByRAM := availableForKV / bpt

	t.Logf("Gemma4 26B Q6_K_XL")
	t.Logf("  File: %d GB, RAM: %d GB, Usable: %d GB, Avail for KV: %d GB",
		st.Size()/(1024*1024*1024), hw.RAM/(1024*1024*1024),
		usableRAM/(1024*1024*1024), availableForKV/(1024*1024*1024))
	t.Logf("  arch=%s, n_ctx_train=%d, n_layers=%d, n_heads=%d, n_kv_heads=%d, head_dim=%d",
		meta.Arch, meta.NCtxTrain, meta.NBlock, meta.NHeads, meta.KVHeads(), meta.HeadDim())
	t.Logf("  KV bpt (f16): %d bytes, Kv bpt (q8_0): %d bytes", bpt, meta.KVCacheBytesPerToken(false))
	t.Logf("  Max ctx by RAM (f16): %d, (q8_0): %d", maxByRAM, availableForKV/meta.KVCacheBytesPerToken(false))

	calc := New(hw)
	r := calc.CalcSingle(modelPath, st.Size(), true)
	t.Logf("  SMART ctx (f16): %d (used_default=%v)", r.CtxSize, r.UsedDefault)

	rQ8 := calc.CalcSingle(modelPath, st.Size(), false)
	t.Logf("  SMART ctx (q8_0): %d", rQ8.CtxSize)

	if r.UsedDefault {
		t.Error("smart calc should not fall back to default for a valid GGUF file")
	}
}
