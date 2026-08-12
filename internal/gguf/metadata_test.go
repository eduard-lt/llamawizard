package gguf

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeTestGGUF(t *testing.T, kvPairs map[string]any) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.gguf")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	data := buildGGUFBytes(t, kvPairs)
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	return path
}

func buildGGUFBytes(t *testing.T, kvPairs map[string]any) []byte {
	t.Helper()

	var buf []byte

	// Magic
	buf = append(buf, []byte("GGUF")...)

	// Version (uint32 LE)
	buf = appendUint32LE(buf, 3)

	// n_tensors (uint64 LE) — 0 for metadata-only test
	buf = appendUint64LE(buf, 0)

	// n_kv (uint64 LE)
	nKV := uint64(len(kvPairs))
	buf = appendUint64LE(buf, nKV)

	for key, val := range kvPairs {
		buf = appendKV(buf, key, val)
	}

	return buf
}

func appendKV(buf []byte, key string, val any) []byte {
	// Key string: uint64 length + bytes
	buf = appendUint64LE(buf, uint64(len(key)))
	buf = append(buf, []byte(key)...)

	switch v := val.(type) {
	case uint32:
		buf = appendUint32LE(buf, 4) // type tag
		buf = appendUint32LE(buf, v)
	case int32:
		buf = appendUint32LE(buf, 5)
		buf = appendInt32LE(buf, v)
	case string:
		buf = appendUint32LE(buf, 8) // STRING
		buf = appendUint64LE(buf, uint64(len(v)))
		buf = append(buf, []byte(v)...)
	case bool:
		buf = appendUint32LE(buf, 7) // BOOL
		b := byte(0)
		if v {
			b = 1
		}
		buf = append(buf, b)
	case uint16:
		buf = appendUint32LE(buf, 2)
		buf = appendUint16LE(buf, v)
	case int16:
		buf = appendUint32LE(buf, 3)
		buf = appendInt16LE(buf, v)
	case uint64:
		buf = appendUint32LE(buf, 10)
		buf = appendUint64LE(buf, v)
	case int64:
		buf = appendUint32LE(buf, 11)
		buf = appendInt64LE(buf, v)
	case float32:
		buf = appendUint32LE(buf, 6)
		buf = appendFloat32LE(buf, v)
	case float64:
		buf = appendUint32LE(buf, 12)
		buf = appendFloat64LE(buf, v)
	case []uint32:
		buf = appendUint32LE(buf, 9) // ARRAY
		buf = appendUint32LE(buf, 4) // element type = UINT32
		buf = appendUint64LE(buf, uint64(len(v)))
		for _, e := range v {
			buf = appendUint32LE(buf, e)
		}
	}
	return buf
}

func TestReadMetadata_LlamaArch(t *testing.T) {
	kv := map[string]any{
		"general.architecture":          "llama",
		"llama.context_length":          uint32(131072),
		"llama.embedding_length":        uint32(4096),
		"llama.block_count":             uint32(32),
		"llama.attention.head_count":    uint32(32),
		"llama.attention.head_count_kv": uint32(8),
	}

	path := makeTestGGUF(t, kv)
	m, err := ReadMetadata(path)
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}

	if m.Arch != "llama" {
		t.Errorf("Arch = %q, want %q", m.Arch, "llama")
	}
	if m.NBlock != 32 {
		t.Errorf("NBlock = %d, want 32", m.NBlock)
	}
	if m.NEmbd != 4096 {
		t.Errorf("NEmbd = %d, want 4096", m.NEmbd)
	}
	if m.NHeads != 32 {
		t.Errorf("NHeads = %d, want 32", m.NHeads)
	}
	if m.NKVHeads != 8 {
		t.Errorf("NKVHeads = %d, want 8", m.NKVHeads)
	}
	if m.NCtxTrain != 131072 {
		t.Errorf("NCtxTrain = %d, want 131072", m.NCtxTrain)
	}

	headDim := m.HeadDim()
	expectedHeadDim := uint32(4096 / 32) // 128
	if headDim != expectedHeadDim {
		t.Errorf("HeadDim() = %d, want %d", headDim, expectedHeadDim)
	}

	if m.KVHeads() != 8 {
		t.Errorf("KVHeads() = %d, want 8", m.KVHeads())
	}

	bpt := m.KVCacheBytesPerToken(true)
	// 32 × 8 × (128+128) × 2 = 32 × 8 × 256 × 2 = 131072
	expectedBPT := uint64(32 * 8 * 256 * 2)
	if bpt != expectedBPT {
		t.Errorf("KVCacheBytesPerToken(f16) = %d, want %d", bpt, expectedBPT)
	}
}

func TestReadMetadata_QwenArch(t *testing.T) {
	kv := map[string]any{
		"general.architecture":          "qwen2",
		"qwen2.context_length":          uint32(32768),
		"qwen2.embedding_length":        uint32(3584),
		"qwen2.block_count":             uint32(28),
		"qwen2.attention.head_count":    uint32(28),
		"qwen2.attention.head_count_kv": uint32(4),
	}

	path := makeTestGGUF(t, kv)
	m, err := ReadMetadata(path)
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}

	if m.Arch != "qwen2" {
		t.Errorf("Arch = %q, want %q", m.Arch, "qwen2")
	}
	if m.NCtxTrain != 32768 {
		t.Errorf("NCtxTrain = %d, want 32768", m.NCtxTrain)
	}

	headDim := m.HeadDim()
	expectedHeadDim := uint32(3584 / 28) // 128
	if headDim != expectedHeadDim {
		t.Errorf("HeadDim() = %d, want %d", headDim, expectedHeadDim)
	}

	bpt := m.KVCacheBytesPerToken(true)
	// 28 × 4 × (128+128) × 2 = 28 × 4 × 256 × 2 = 57344
	expectedBPT := uint64(28 * 4 * 256 * 2)
	if bpt != expectedBPT {
		t.Errorf("KVCacheBytesPerToken(f16) = %d, want %d", bpt, expectedBPT)
	}
}

func TestReadMetadata_ExplicitHeadDim(t *testing.T) {
	kv := map[string]any{
		"general.architecture":        "gemma2",
		"gemma2.context_length":       uint32(8192),
		"gemma2.embedding_length":     uint32(2048),
		"gemma2.block_count":          uint32(18),
		"gemma2.attention.head_count": uint32(8),
		"gemma2.attention.head_dim":   uint32(256),
	}

	path := makeTestGGUF(t, kv)
	m, err := ReadMetadata(path)
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}

	headDim := m.HeadDim()
	if headDim != 256 {
		t.Errorf("HeadDim() = %d, want 256 (explicit head_dim)", headDim)
	}
}

func TestReadMetadata_ExplicitKeyValueLengths(t *testing.T) {
	kv := map[string]any{
		"general.architecture":         "phi3",
		"phi3.context_length":          uint32(128000),
		"phi3.embedding_length":        uint32(3072),
		"phi3.block_count":             uint32(32),
		"phi3.attention.head_count":    uint32(32),
		"phi3.attention.head_count_kv": uint32(32),
		"phi3.attention.key_length":    uint32(96),
		"phi3.attention.value_length":  uint32(96),
	}

	path := makeTestGGUF(t, kv)
	m, err := ReadMetadata(path)
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}

	if m.HeadDimK != 96 {
		t.Errorf("HeadDimK = %d, want 96", m.HeadDimK)
	}
	if m.HeadDimV != 96 {
		t.Errorf("HeadDimV = %d, want 96", m.HeadDimV)
	}
	if m.HeadDim() != 96 {
		t.Errorf("HeadDim() = %d, want 96", m.HeadDim())
	}
}

func TestReadMetadata_KVHeadsDefaultToHeads(t *testing.T) {
	kv := map[string]any{
		"general.architecture":       "llama",
		"llama.context_length":       uint32(32768),
		"llama.embedding_length":     uint32(4096),
		"llama.block_count":          uint32(32),
		"llama.attention.head_count": uint32(32),
	}

	path := makeTestGGUF(t, kv)
	m, err := ReadMetadata(path)
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}

	if m.KVHeads() != 32 {
		t.Errorf("KVHeads() = %d, want 32 (default to n_heads)", m.KVHeads())
	}
}

func TestReadMetadata_NotAGGUFFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not_gguf.txt")
	notGGUF := make([]byte, 30)
	copy(notGGUF, "XXXX")
	if err := os.WriteFile(path, notGGUF, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ReadMetadata(path)
	if err == nil {
		t.Error("expected error for non-GGUF file")
	}
	if !strings.Contains(err.Error(), "valid GGUF") {
		t.Errorf("expected 'valid GGUF' in error, got: %v", err)
	}
}

func TestReadMetadata_MissingArchitecture(t *testing.T) {
	kv := map[string]any{
		"llama.context_length": uint32(32768),
	}

	path := makeTestGGUF(t, kv)
	_, err := ReadMetadata(path)
	if err == nil {
		t.Error("expected error for missing general.architecture")
	}
}

func TestReadMetadata_MaxCtxByRAM(t *testing.T) {
	kv := map[string]any{
		"general.architecture":          "llama",
		"llama.context_length":          uint32(131072),
		"llama.embedding_length":        uint32(4096),
		"llama.block_count":             uint32(32),
		"llama.attention.head_count":    uint32(32),
		"llama.attention.head_count_kv": uint32(8),
	}

	path := makeTestGGUF(t, kv)
	m, err := ReadMetadata(path)
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}

	// bpt = 131072 for f16
	bpt := m.KVCacheBytesPerToken(true)
	if bpt != 131072 {
		t.Fatalf("unexpected bpt: %d", bpt)
	}

	// With 10GB available: 10*1024*1024*1024 / 131072 ≈ 81920
	availableRAM := uint64(10 * 1024 * 1024 * 1024)
	maxCtx := m.MaxCtxByRAM(availableRAM, true)
	expected := availableRAM / bpt
	if maxCtx != expected {
		t.Errorf("MaxCtxByRAM = %d, want %d", maxCtx, expected)
	}
}

func TestKVCacheBytesPerToken_Q8_0(t *testing.T) {
	kv := map[string]any{
		"general.architecture":          "llama",
		"llama.context_length":          uint32(131072),
		"llama.embedding_length":        uint32(4096),
		"llama.block_count":             uint32(32),
		"llama.attention.head_count":    uint32(32),
		"llama.attention.head_count_kv": uint32(8),
	}

	path := makeTestGGUF(t, kv)
	m, err := ReadMetadata(path)
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}

	bptF16 := m.KVCacheBytesPerToken(true)
	bptQ8 := m.KVCacheBytesPerToken(false)

	if bptQ8*2 != bptF16 {
		t.Errorf("q8_0 bpt %d should be half of f16 bpt %d", bptQ8, bptF16)
	}
}

// Regression test: assert KVCacheBytesPerToken against independently
// verified reference values for well-known real models.
//
// These expected values are hand-computed from the model architecture
// parameters and are NOT derived from the function under test.
// A bug in the formula or type_size constant will cause a mismatch even
// if the synthetic test headers are internally consistent.
func TestKVCacheBytesPerToken_RealModelReferenceValues(t *testing.T) {
	tests := []struct {
		name     string
		arch     string
		kvPairs  map[string]any
		wantF16  uint64
		wantQ8_0 uint64
		reason   string
	}{
		{
			name: "Llama-3-8B (GQA, head_dim=128)",
			arch: "llama",
			kvPairs: map[string]any{
				"llama.block_count":             uint32(32),
				"llama.embedding_length":        uint32(4096),
				"llama.attention.head_count":    uint32(32),
				"llama.attention.head_count_kv": uint32(8),
				"llama.context_length":          uint32(131072),
			},
			// 32 × 8 × (128+128) × 2 = 131072 bytes/token
			// 32 × 8 × (128+128) × 1 = 65536 bytes/token
			wantF16:  131072,
			wantQ8_0: 65536,
			reason:   "well-known reference: 128 KB/token f16, 64 KB/token q8_0",
		},
		{
			name: "Llama-3-70B (GQA, head_dim=128)",
			arch: "llama",
			kvPairs: map[string]any{
				"llama.block_count":             uint32(80),
				"llama.embedding_length":        uint32(8192),
				"llama.attention.head_count":    uint32(64),
				"llama.attention.head_count_kv": uint32(8),
				"llama.context_length":          uint32(131072),
			},
			// head_dim = 8192/64 = 128
			// 80 × 8 × (128+128) × 2 = 327680 bytes/token
			// 80 × 8 × (128+128) × 1 = 163840 bytes/token
			wantF16:  327680,
			wantQ8_0: 163840,
			reason:   "80 layers, 8 KV heads, 128 head_dim: 320 KB/token f16",
		},
		{
			name: "Qwen2.5-7B (GQA, head_dim=128)",
			arch: "qwen2",
			kvPairs: map[string]any{
				"qwen2.block_count":             uint32(28),
				"qwen2.embedding_length":        uint32(3584),
				"qwen2.attention.head_count":    uint32(28),
				"qwen2.attention.head_count_kv": uint32(4),
				"qwen2.context_length":          uint32(131072),
			},
			// head_dim = 3584/28 = 128
			// 28 × 4 × (128+128) × 2 = 57344 bytes/token
			// 28 × 4 × (128+128) × 1 = 28672 bytes/token
			wantF16:  57344,
			wantQ8_0: 28672,
			reason:   "28 layers, 4 KV heads, 128 head_dim: 56 KB/token f16",
		},
		{
			name: "Phi-3-mini (MHA, no GQA, head_dim=96 from key_length)",
			arch: "phi3",
			kvPairs: map[string]any{
				"phi3.block_count":             uint32(32),
				"phi3.embedding_length":        uint32(3072),
				"phi3.attention.head_count":    uint32(32),
				"phi3.attention.head_count_kv": uint32(32),
				"phi3.attention.key_length":    uint32(96),
				"phi3.attention.value_length":  uint32(96),
				"phi3.context_length":          uint32(128000),
			},
			// 32 × 32 × (96+96) × 2 = 393216 bytes/token
			// 32 × 32 × (96+96) × 1 = 196608 bytes/token
			wantF16:  393216,
			wantQ8_0: 196608,
			reason:   "explicit key_length=96, full MHA (32 KV heads): 384 KB/token f16",
		},
		{
			name: "DeepSeek-V2-Lite (MLA with large head_dim)",
			arch: "deepseek2",
			kvPairs: map[string]any{
				"deepseek2.block_count":             uint32(27),
				"deepseek2.embedding_length":        uint32(2048),
				"deepseek2.attention.head_count":    uint32(16),
				"deepseek2.attention.head_count_kv": uint32(16),
				"deepseek2.attention.head_dim":      uint32(192),
				"deepseek2.context_length":          uint32(163840),
			},
			// head_dim explicitly set to 192, overrides embedding/heads
			// 27 × 16 × (192+192) × 2 = 331776 bytes/token
			// 27 × 16 × (192+192) × 1 = 165888 bytes/token
			wantF16:  331776,
			wantQ8_0: 165888,
			reason:   "explicit head_dim=192 with full MHA: 324 KB/token f16",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kv := map[string]any{
				"general.architecture": tt.arch,
			}
			for k, v := range tt.kvPairs {
				kv[k] = v
			}

			path := makeTestGGUF(t, kv)
			m, err := ReadMetadata(path)
			if err != nil {
				t.Fatalf("ReadMetadata: %v", err)
			}

			gotF16 := m.KVCacheBytesPerToken(true)
			if gotF16 != tt.wantF16 {
				t.Errorf("KVCacheBytesPerToken(f16) = %d, want %d (%s)",
					gotF16, tt.wantF16, tt.reason)
			}

			gotQ8 := m.KVCacheBytesPerToken(false)
			if gotQ8 != tt.wantQ8_0 {
				t.Errorf("KVCacheBytesPerToken(q8_0) = %d, want %d (%s)",
					gotQ8, tt.wantQ8_0, tt.reason)
			}
		})
	}
}

// Helper functions for LE encoding
func appendUint16LE(buf []byte, v uint16) []byte {
	return append(buf, byte(v), byte(v>>8))
}

func appendInt16LE(buf []byte, v int16) []byte {
	return appendUint16LE(buf, uint16(v))
}

func appendUint32LE(buf []byte, v uint32) []byte {
	return append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func appendInt32LE(buf []byte, v int32) []byte {
	return appendUint32LE(buf, uint32(v))
}

func appendUint64LE(buf []byte, v uint64) []byte {
	return append(buf,
		byte(v), byte(v>>8), byte(v>>16), byte(v>>24),
		byte(v>>32), byte(v>>40), byte(v>>48), byte(v>>56),
	)
}

func appendInt64LE(buf []byte, v int64) []byte {
	return appendUint64LE(buf, uint64(v))
}

func appendFloat32LE(buf []byte, v float32) []byte {
	bits := math.Float32bits(v)
	return append(buf,
		byte(bits), byte(bits>>8), byte(bits>>16), byte(bits>>24),
	)
}

func appendFloat64LE(buf []byte, v float64) []byte {
	bits := math.Float64bits(v)
	return append(buf,
		byte(bits), byte(bits>>8), byte(bits>>16), byte(bits>>24),
		byte(bits>>32), byte(bits>>40), byte(bits>>48), byte(bits>>56),
	)
}
