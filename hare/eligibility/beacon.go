package eligibility

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
)

const nilVal = 0

type patternProvider interface {
	GetPatternId(layer types.LayerID) (int, error)
}

type beacon struct {
	patternProvider patternProvider
}

func newBeacon(patternProvider patternProvider) *beacon {
	return &beacon{
		patternProvider: patternProvider,
	}
}

func (b *beacon) Value(layer types.LayerID) (int, error) {
	v, err := b.patternProvider.GetPatternId(layer)
	if err != nil {
		log.Error("Could not get pattern id: %v", err)
		return nilVal, err
	}

	return v, nil
}
