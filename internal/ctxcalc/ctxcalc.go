package ctxcalc

import (
	"os"

	"github.com/eduard-lt/llamawizard/internal/gguf"
	"github.com/eduard-lt/llamawizard/internal/hardware"
)

const (
	// DefaultCtxSize is used as fallback when smart calculation is unavailable.
	DefaultCtxSize = 32768

	// MaxSaneCtxSize is a hard cap — no context size will exceed this regardless
	// of available RAM or n_ctx_train.
	MaxSaneCtxSize = 131072

	// MinCtxSize prevents absurdly small context sizes.
	MinCtxSize = 4096
)

// ModelBudget tracks the allocated context budget for a single model.
type ModelBudget struct {
	Slug      string
	ModelPath string
	CtxSize   int
	KVBPT     uint64 // KV cache bytes per token
	NCtxTrain uint32
}

// Result holds the outcome of smart context sizing for one model.
type Result struct {
	CtxSize     int    // final context size
	NCtxTrain   uint32 // from GGUF metadata
	KVBPT       uint64 // KV cache bytes per token
	MaxByRAM    uint64 // max context by RAM alone
	UsedDefault bool   // true if fallback to DefaultCtxSize
}

// Calculator computes smart context sizes for models based on GGUF metadata
// and available system RAM.
type Calculator struct {
	hw hardware.HardwareInfo
}

// New creates a Calculator with the given hardware info.
func New(hw hardware.HardwareInfo) *Calculator {
	return &Calculator{hw: hw}
}

// CalcSingle computes the optimal context size for a single model file.
//
// Steps:
//  1. Parse GGUF metadata from the model file to get n_ctx_train, n_layers,
//     n_kv_heads, and head_dim.
//  2. Compute KV cache bytes per token.
//  3. Determine usable RAM budget (total RAM minus safety margin).
//  4. Subtract the model's RAM footprint from the budget to get RAM available
//     for KV cache. The footprint is modelSizeBytes, or the stat'd file size
//     when that is unknown (the file must exist to be parsed). If the model
//     alone meets or exceeds the usable budget, the KV budget is zero — not
//     "all of RAM" — because there is nothing left for the cache.
//  5. Solve max_ctx = available_for_kv / kv_bytes_per_token.
//  6. Clamp to [MinCtxSize, min(MaxSaneCtxSize, n_ctx_train)].
//  7. Snap to the nearest power-of-2 value.
//
// If GGUF metadata can't be read, returns DefaultCtxSize with UsedDefault=true.
func (c *Calculator) CalcSingle(modelPath string, modelSizeBytes int64, f16KVCache bool) Result {
	meta, err := gguf.ReadMetadata(modelPath)
	if err != nil {
		return Result{
			CtxSize:     DefaultCtxSize,
			UsedDefault: true,
		}
	}

	bpt := meta.KVCacheBytesPerToken(f16KVCache)

	// Model footprint in RAM. When the caller doesn't know the size (e.g.
	// --link with a failed HEAD request), stat the file: it exists, since
	// ReadMetadata just parsed it.
	footprint := modelSizeBytes
	if footprint <= 0 {
		if st, err := os.Stat(modelPath); err == nil {
			footprint = st.Size()
		}
	}

	usableRAM := hardware.UsableRAMBudget(c.hw.RAM)

	// RAM left for the KV cache. If the model alone meets or exceeds the
	// usable budget, nothing is left — flooring at zero instead of
	// "all of RAM" avoids sizing a context that cannot possibly fit.
	availableForKV := int64(0)
	if int64(usableRAM) > footprint {
		availableForKV = int64(usableRAM) - footprint
	}

	var maxByRAM uint64
	if bpt > 0 && availableForKV > 0 {
		maxByRAM = uint64(availableForKV) / bpt
	}

	upperBound := uint64(MaxSaneCtxSize)
	if meta.NCtxTrain > 0 && uint64(meta.NCtxTrain) < upperBound {
		upperBound = uint64(meta.NCtxTrain)
	}

	ctx := maxByRAM
	if ctx > upperBound {
		ctx = upperBound
	}
	if ctx < MinCtxSize {
		ctx = MinCtxSize
	}

	// snapToPowerOf2 never returns below MinCtxSize, so no re-clamp is needed.
	ctx = snapToPowerOf2(ctx)

	return Result{
		CtxSize:   int(ctx),
		NCtxTrain: meta.NCtxTrain,
		KVBPT:     bpt,
		MaxByRAM:  maxByRAM,
	}
}

// snapToPowerOf2 rounds down to the nearest power-of-2 boundary
// (4096, 8192, 16384, 32768, 65536, 131072).
func snapToPowerOf2(n uint64) uint64 {
	candidates := []uint64{4096, 8192, 16384, 32768, 65536, 131072}
	for i := len(candidates) - 1; i >= 0; i-- {
		if n >= candidates[i] {
			return candidates[i]
		}
	}
	return 4096
}
