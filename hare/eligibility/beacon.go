package eligibility

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
)

const nilVal = 0

type patternProvider interface {
	// Returns the pattern id of the given layer
	// the pattern id is defined to be the hash of blocks in a layer
	GetPatternId(layer types.LayerID) (uint32, error)
}

type beacon struct {
	// provides a value that is unpredictable and agreed (w.h.p.) by all honest
	patternProvider patternProvider
}

func newBeacon(patternProvider patternProvider) *beacon {
	return &beacon{
		patternProvider: patternProvider,
	}
}

// Returns the unpredictable and agreed value for the given layer
func (b *beacon) Value(layer types.LayerID) (uint32, error) {
	v, err := b.patternProvider.GetPatternId(layer)
	if err != nil {
		log.Error("Could not get pattern id: %v", err)
		return nilVal, err
	}

	return v, nil
}
