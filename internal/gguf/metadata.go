package gguf

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// Metadata holds the essential GGUF parameters needed for KV cache estimation.
type Metadata struct {
	Arch      string // e.g. "llama", "qwen2"
	NBlock    uint32 // number of transformer blocks (layers)
	NEmbd     uint32 // embedding dimension
	NHeads    uint32 // number of attention heads
	NKVHeads  uint32 // number of KV heads (0 if not present, defaults to NHeads)
	HeadDimK  uint32 // key head dimension (0 if not explicitly set)
	HeadDimV  uint32 // value head dimension (0 if not explicitly set)
	NCtxTrain uint32 // native training context length
}

// HeadDim derives the per-head dimension for K and V.
// If explicit key/value lengths are set in metadata, they are used;
// otherwise falls back to n_embd / n_heads.
func (m Metadata) HeadDim() uint32 {
	if m.HeadDimK > 0 {
		return m.HeadDimK
	}
	if m.NHeads == 0 {
		return 0
	}
	return m.NEmbd / m.NHeads
}

// KVHeads returns n_kv_heads, falling back to n_heads if not set.
func (m Metadata) KVHeads() uint32 {
	if m.NKVHeads > 0 {
		return m.NKVHeads
	}
	return m.NHeads
}

// KVCacheBytesPerToken returns the number of bytes consumed per token for both
// K and V caches.
//
//	kv_bytes = n_layers × n_kv_heads × (head_dim_k + head_dim_v) × type_size
//
// type_size = 2 for f16 KV cache, 1 for q8_0 quantized.
//
// f16KV cache (f16KVCache=true) is conservative: zero quality loss on K/V
// values but uses 2 bytes per element. q8_0 KV cache (f16KVCache=false)
// halving the per-token cost at the cost of slight quantization noise on
// key/value tensors — generally safe and widely used, but if quality degrades
// at very long contexts (>100K), switching back to f16 is the first knob to
// turn.
func (m Metadata) KVCacheBytesPerToken(f16KVCache bool) uint64 {
	bytesPerElement := uint64(1)
	if f16KVCache {
		bytesPerElement = 2
	}

	headDimK := m.HeadDim()
	headDimV := m.HeadDim()
	if m.HeadDimV > 0 {
		headDimV = m.HeadDimV
	}

	if headDimK == 0 || m.NBlock == 0 {
		return 0
	}

	kvHeads := m.KVHeads()
	return uint64(m.NBlock) * uint64(kvHeads) * uint64(headDimK+headDimV) * bytesPerElement
}

// MaxCtxByRAM returns the maximum context size that fits in available RAM.
// availableRAMBytes is the RAM available for KV cache (usable - model weights).
// Returns 0 if metadata is insufficient.
func (m Metadata) MaxCtxByRAM(availableRAMBytes uint64, f16KVCache bool) uint64 {
	bpt := m.KVCacheBytesPerToken(f16KVCache)
	if bpt == 0 {
		return 0
	}
	return availableRAMBytes / bpt
}

// ReadMetadata reads GGUF metadata from a file by its path.
// Only the header + metadata section is read (not tensor data).
func ReadMetadata(path string) (Metadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return Metadata{}, fmt.Errorf("opening gguf file: %w", err)
	}
	defer func() { _ = f.Close() }()

	return readMetadata(f)
}

// readMetadata parses the GGUF header and metadata from a reader.
func readMetadata(r io.Reader) (Metadata, error) {
	var header [24]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Metadata{}, fmt.Errorf("reading gguf header: %w", err)
	}

	if string(header[0:4]) != "GGUF" {
		return Metadata{}, fmt.Errorf("not a valid GGUF file: magic %q", string(header[0:4]))
	}

	version := binary.LittleEndian.Uint32(header[4:8])
	if version < 2 || version > 3 {
		return Metadata{}, fmt.Errorf("unsupported GGUF version %d (expected 2 or 3)", version)
	}

	nTensors := binary.LittleEndian.Uint64(header[8:16])
	nKV := binary.LittleEndian.Uint64(header[16:24])
	_ = nTensors

	kv := make(map[string]any, nKV)
	for i := uint64(0); i < nKV; i++ {
		key, val, err := readKV(r)
		if err != nil {
			return Metadata{}, fmt.Errorf("reading kv pair %d: %w", i, err)
		}
		kv[key] = val
	}

	return buildMetadata(kv)
}

func readKV(r io.Reader) (string, any, error) {
	keyLen, err := readUint64(r)
	if err != nil {
		return "", nil, fmt.Errorf("key length: %w", err)
	}

	keyBytes := make([]byte, keyLen)
	if _, err := io.ReadFull(r, keyBytes); err != nil {
		return "", nil, fmt.Errorf("key bytes: %w", err)
	}
	key := string(keyBytes)

	var vt uint32
	if err := binary.Read(r, binary.LittleEndian, &vt); err != nil {
		return "", nil, fmt.Errorf("value type: %w", err)
	}

	val, err := readValue(r, vt)
	if err != nil {
		return "", nil, fmt.Errorf("value for %q: %w", key, err)
	}

	return key, val, nil
}

func readValue(r io.Reader, vt uint32) (any, error) {
	switch vt {
	case 0: // UINT8
		var v uint8
		return v, binary.Read(r, binary.LittleEndian, &v)
	case 1: // INT8
		var v int8
		return v, binary.Read(r, binary.LittleEndian, &v)
	case 2: // UINT16
		var v uint16
		return v, binary.Read(r, binary.LittleEndian, &v)
	case 3: // INT16
		var v int16
		return v, binary.Read(r, binary.LittleEndian, &v)
	case 4: // UINT32
		var v uint32
		return v, binary.Read(r, binary.LittleEndian, &v)
	case 5: // INT32
		var v int32
		return v, binary.Read(r, binary.LittleEndian, &v)
	case 6: // FLOAT32
		var v float32
		return v, binary.Read(r, binary.LittleEndian, &v)
	case 7: // BOOL
		var v uint8
		err := binary.Read(r, binary.LittleEndian, &v)
		return v != 0, err
	case 8: // STRING
		return readString(r)
	case 9: // ARRAY
		return readArray(r)
	case 10: // UINT64
		var v uint64
		return v, binary.Read(r, binary.LittleEndian, &v)
	case 11: // INT64
		var v int64
		return v, binary.Read(r, binary.LittleEndian, &v)
	case 12: // FLOAT64
		var v float64
		return v, binary.Read(r, binary.LittleEndian, &v)
	default:
		return nil, fmt.Errorf("unknown value type %d", vt)
	}
}

func readString(r io.Reader) (string, error) {
	length, err := readUint64(r)
	if err != nil {
		return "", err
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func readArray(r io.Reader) ([]any, error) {
	var elemType uint32
	if err := binary.Read(r, binary.LittleEndian, &elemType); err != nil {
		return nil, err
	}
	n, err := readUint64(r)
	if err != nil {
		return nil, err
	}

	result := make([]any, n)
	for i := uint64(0); i < n; i++ {
		val, err := readValue(r, elemType)
		if err != nil {
			return nil, fmt.Errorf("array element %d: %w", i, err)
		}
		result[i] = val
	}
	return result, nil
}

func readUint64(r io.Reader) (uint64, error) {
	var v uint64
	err := binary.Read(r, binary.LittleEndian, &v)
	return v, err
}

func buildMetadata(kv map[string]any) (Metadata, error) {
	var m Metadata

	arch, ok := kv["general.architecture"]
	if !ok {
		return Metadata{}, fmt.Errorf("missing general.architecture in GGUF metadata")
	}
	archStr, ok := arch.(string)
	if !ok {
		return Metadata{}, fmt.Errorf("general.architecture is not a string")
	}
	m.Arch = archStr

	prefix := archStr + "."

	getUint32 := func(key string) uint32 {
		val, ok := kv[prefix+key]
		if !ok {
			return 0
		}
		switch v := val.(type) {
		case uint32:
			return v
		case int32:
			if v < 0 {
				return 0
			}
			return uint32(v)
		case uint16:
			return uint32(v)
		case int16:
			if v < 0 {
				return 0
			}
			return uint32(v)
		case uint64:
			if v > 0xFFFFFFFF {
				return 0xFFFFFFFF
			}
			return uint32(v)
		case int64:
			if v < 0 || v > 0xFFFFFFFF {
				return 0
			}
			return uint32(v)
		default:
			return 0
		}
	}

	m.NBlock = getUint32("block_count")
	m.NEmbd = getUint32("embedding_length")
	m.NHeads = getUint32("attention.head_count")
	m.NKVHeads = getUint32("attention.head_count_kv")
	m.NCtxTrain = getUint32("context_length")

	headDim := getUint32("attention.head_dim")
	if headDim > 0 {
		m.HeadDimK = headDim
		m.HeadDimV = headDim
	} else {
		m.HeadDimK = getUint32("attention.key_length")
		m.HeadDimV = getUint32("attention.value_length")
	}

	return m, nil
}
