package types

import (
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/hash"
)

var abstainSentinel = []byte{0}

// NewOpinionHasher returns new instance of the opinion hasher.
func NewOpinionHasher() *OpinionHasher {
	hasher := hash.New()
	return &OpinionHasher{enc: scale.NewEncoder(hasher), h: hasher}
}

// OpinionHasher implements utility struct for computing opinion hash.
type OpinionHasher struct {
	enc *scale.Encoder
	h   hash.Hash
}

// WritePrevious aggregated hash.
func (h *OpinionHasher) WritePrevious(hash Hash32) error {
	_, err := h.h.Write(hash[:])
	return err
}

// WriteAbstain writes abstain sentinel as an opinion.
func (h *OpinionHasher) WriteAbstain() error {
	_, err := h.h.Write(abstainSentinel)
	return err
}

// WriteSupport writes id and height of the block.
func (h *OpinionHasher) WriteSupport(id BlockID, height uint64) error {
	_, err := scale.EncodeByteArray(h.enc, id[:])
	if err != nil {
		return err
	}
	_, err = scale.EncodeUint64(h.enc, height)
	return err
}

// Sum appends hash sum to dst.
func (h *OpinionHasher) Sum(dst []byte) []byte {
	return h.h.Sum(dst)
}

// Reset opinion hasher state.
func (h *OpinionHasher) Reset() {
	h.h.Reset()
}
