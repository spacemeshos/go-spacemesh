package opinionhash

import (
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
)

var abstainSentinel = []byte{0}

// New returns new instance of the opinion hasher.
func New() *OpinionHasher {
	hasher := hash.New()
	return &OpinionHasher{enc: scale.NewEncoder(hasher), h: hasher}
}

// OpinionHasher is a utility for computing opinion hash.
type OpinionHasher struct {
	enc *scale.Encoder
	h   hash.Hash
}

// WritePrevious aggregated hash.
func (h *OpinionHasher) WritePrevious(hash types.Hash32) {
	_, err := h.h.Write(hash[:])
	if err != nil {
		panic("unexpected hash write failure: " + err.Error())
	}
}

// WriteAbstain writes abstain sentinel as an opinion.
func (h *OpinionHasher) WriteAbstain() {
	_, err := h.h.Write(abstainSentinel)
	if err != nil {
		panic("unexpected hash write failure: " + err.Error())
	}
}

// WriteSupport writes id and height of the block.
func (h *OpinionHasher) WriteSupport(id types.BlockID, height uint64) {
	_, err := scale.EncodeByteArray(h.enc, id[:])
	if err != nil {
		if err != nil {
			panic("unexpected scale encode failure: " + err.Error())
		}
	}
	_, err = scale.EncodeUint64(h.enc, height)
	if err != nil {
		panic("unexpected scale encode failure: " + err.Error())
	}
}

// Sum appends hash sum to dst.
func (h *OpinionHasher) Sum(dst []byte) []byte {
	return h.h.Sum(dst)
}

// Hash instantiates 32bytes and write hash.Sum to it.
func (h *OpinionHasher) Hash() (rst types.Hash32) {
	h.Sum(rst[:0])
	return rst
}

// Reset opinion hasher state.
func (h *OpinionHasher) Reset() {
	h.h.Reset()
}
