package eligibility

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
)

const nilVal = 0

type patternProvider interface {
	// GetPatternId returns the pattern Id of the given Layer
	// the pattern Id is defined to be the hash of blocks in a Layer
	GetGoodPattern(layer types.LayerID) (uint32, error)
}

type beacon struct {
	// provides a value that is unpredictable and agreed (w.h.p.) by all honest
	patternProvider patternProvider
}

func NewBeacon(patternProvider patternProvider) *beacon {
	return &beacon{
		patternProvider: patternProvider,
	}
}

// Value returns the unpredictable and agreed value for the given Layer
func (b *beacon) Value(layer types.LayerID) (uint32, error) {
	v, err := b.patternProvider.GetGoodPattern(layer)
	if err != nil {
		log.Error("Could not get pattern Id: %v", err)
		return nilVal, err
	}

	return v, nil
}
