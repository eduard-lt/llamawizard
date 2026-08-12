package ctxcalc

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eduard-lt/llamawizard/internal/hardware"
)

func makeTestGGUF(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "model.gguf")

	data := []byte{
		// Magic "GGUF"
		'G', 'G', 'U', 'F',
		// Version 3
		3, 0, 0, 0,
		// n_tensors = 0
		0, 0, 0, 0, 0, 0, 0, 0,
		// n_kv = 7
		7, 0, 0, 0, 0, 0, 0, 0,
	}

	appendStr := func(key string) {
		data = appendU64LE(data, uint64(len(key)))
		data = append(data, []byte(key)...)
	}

	// general.architecture = "llama"
	appendStr("general.architecture")
	data = appendU32LE(data, 8) // STRING
	data = appendU64LE(data, 5)
	data = append(data, []byte("llama")...)

	// llama.context_length = 131072
	appendStr("llama.context_length")
	data = appendU32LE(data, 4) // UINT32
	data = appendU32LE(data, 131072)

	// llama.embedding_length = 4096
	appendStr("llama.embedding_length")
	data = appendU32LE(data, 4)
	data = appendU32LE(data, 4096)

	// llama.block_count = 32
	appendStr("llama.block_count")
	data = appendU32LE(data, 4)
	data = appendU32LE(data, 32)

	// llama.attention.head_count = 32
	appendStr("llama.attention.head_count")
	data = appendU32LE(data, 4)
	data = appendU32LE(data, 32)

	// llama.attention.head_count_kv = 8
	appendStr("llama.attention.head_count_kv")
	data = appendU32LE(data, 4)
	data = appendU32LE(data, 8)

	// llama.attention.key_length = 128
	appendStr("llama.attention.key_length")
	data = appendU32LE(data, 4)
	data = appendU32LE(data, 128)

	// llama.attention.value_length = 128
	appendStr("llama.attention.value_length")
	data = appendU32LE(data, 4)
	data = appendU32LE(data, 128)

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()
	return path
}

func appendU32LE(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func appendU64LE(b []byte, v uint64) []byte {
	return append(b,
		byte(v), byte(v>>8), byte(v>>16), byte(v>>24),
		byte(v>>32), byte(v>>40), byte(v>>48), byte(v>>56),
	)
}

func TestSnapToPowerOf2(t *testing.T) {
	tests := []struct {
		in   uint64
		want uint64
	}{
		{1000, 4096},
		{5000, 4096},
		{8192, 8192},
		{10000, 8192},
		{16000, 8192},
		{16384, 16384},
		{20000, 16384},
		{32000, 16384},
		{32768, 32768},
		{40000, 32768},
		{65000, 32768},
		{65536, 65536},
		{100000, 65536},
		{131072, 131072},
		{200000, 131072},
	}
	for _, tt := range tests {
		got := snapToPowerOf2(tt.in)
		if got != tt.want {
			t.Errorf("snapToPowerOf2(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestCalcSingle_HappyPath(t *testing.T) {
	hw := hardware.HardwareInfo{
		RAM: 48 * 1024 * 1024 * 1024, // 48 GB
	}

	calc := New(hw)

	modelPath := makeTestGGUF(t)
	modelSizeBytes := int64(20 * 1024 * 1024 * 1024) // 20 GB model

	r := calc.CalcSingle(modelPath, modelSizeBytes, true)

	if r.UsedDefault {
		t.Error("should not use default for valid GGUF")
	}

	// Usable RAM: 48GB - max(8GB, 48*0.15=7.2GB) = 40GB
	// Available for KV: 40GB - 20GB = 20GB
	// bpt = 32 × 8 × (128+128) × 2 = 131072
	// max_ctx = 20GB / 131072 = 163840
	// upperBound = min(131072, 131072) = 131072
	// snap(131072) = 131072
	if r.CtxSize != 131072 {
		t.Errorf("CtxSize = %d, want 131072 (bpt=%d, maxByRAM=%d)", r.CtxSize, r.KVBPT, r.MaxByRAM)
	}

	if r.NCtxTrain != 131072 {
		t.Errorf("NCtxTrain = %d, want 131072", r.NCtxTrain)
	}

	if r.KVBPT != 131072 {
		t.Errorf("KVBPT = %d, want 131072", r.KVBPT)
	}
}

func TestCalcSingle_MissingFileFallsBackToDefault(t *testing.T) {
	hw := hardware.HardwareInfo{RAM: 48 * 1024 * 1024 * 1024}
	calc := New(hw)

	r := calc.CalcSingle("/nonexistent/model.gguf", 0, true)

	if !r.UsedDefault {
		t.Error("should use default for missing file")
	}
	if r.CtxSize != DefaultCtxSize {
		t.Errorf("CtxSize = %d, want %d", r.CtxSize, DefaultCtxSize)
	}
}

func TestCalcSingle_CapsAtCtxTrain(t *testing.T) {
	hw := hardware.HardwareInfo{
		RAM: 256 * 1024 * 1024 * 1024, // 256 GB (tons of RAM)
	}

	calc := New(hw)

	// Model with n_ctx_train=32768
	dir := t.TempDir()
	path := filepath.Join(dir, "cap.gguf")

	data := []byte{
		'G', 'G', 'U', 'F',
		3, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		5, 0, 0, 0, 0, 0, 0, 0,
	}

	addKV := func(key string, v uint32) {
		data = appendU64LE(data, uint64(len(key)))
		data = append(data, []byte(key)...)
		data = appendU32LE(data, 4) // UINT32
		data = appendU32LE(data, v)
	}

	addKVS := func(key, val string) {
		data = appendU64LE(data, uint64(len(key)))
		data = append(data, []byte(key)...)
		data = appendU32LE(data, 8) // STRING
		data = appendU64LE(data, uint64(len(val)))
		data = append(data, []byte(val)...)
	}

	addKVS("general.architecture", "llama")
	addKV("llama.context_length", 32768)
	addKV("llama.embedding_length", 4096)
	addKV("llama.block_count", 32)
	addKV("llama.attention.head_count", 32)
	addKV("llama.attention.head_count_kv", 8)

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write(data)
	_ = f.Close()

	r := calc.CalcSingle(path, int64(1*1024*1024*1024), true)

	if r.CtxSize > int(r.NCtxTrain) {
		t.Errorf("CtxSize %d exceeds n_ctx_train %d", r.CtxSize, r.NCtxTrain)
	}
	if r.CtxSize != 32768 {
		t.Errorf("expected 32768, got %d", r.CtxSize)
	}
}

func TestCalcSingle_ClampsToMin(t *testing.T) {
	hw := hardware.HardwareInfo{
		RAM: 4 * 1024 * 1024 * 1024, // 4 GB (tiny)
	}

	calc := New(hw)
	modelPath := makeTestGGUF(t)
	modelSizeBytes := int64(3 * 1024 * 1024 * 1024) // 3 GB model leaves very little room

	r := calc.CalcSingle(modelPath, modelSizeBytes, true)

	if r.CtxSize < MinCtxSize {
		t.Errorf("CtxSize %d below MinCtxSize %d", r.CtxSize, MinCtxSize)
	}
}
