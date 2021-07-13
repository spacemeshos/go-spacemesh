package tortoise

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type vec [2]int

const (
	globalThreshold = 0.6
)

var ( //correction vectors type
	// Opinion vectors
	support = vec{1, 0}
	against = vec{0, 1}
	abstain = vec{0, 0}
)

// Field returns a log field. Implements the LoggableField interface.
func (a vec) Field() log.Field {
	return log.String("vote_vector", fmt.Sprint(a))
}

func (a vec) Add(v vec) vec {
	return vec{a[0] + v[0], a[1] + v[1]}
}

func (a vec) Negate() vec {
	a[0] = a[0] * -1
	a[1] = a[1] * -1
	return a
}

func (a vec) Multiply(x int) vec {
	a[0] = a[0] * x
	a[1] = a[1] * x
	return a
}

func simplifyVote(v vec) vec {
	if v[0] > v[1] {
		return support
	}

	if v[1] > v[0] {
		return against
	}

	return abstain
}

func (a vec) String() string {
	v := simplifyVote(a)

	if v == support {
		return "support"
	}

	if v == against {
		return "against"
	}

	return "abstain"
}

func calculateGlobalOpinion(logger log.Log, v vec, layerSize int, delta float64) vec {
	threshold := globalThreshold * delta * float64(layerSize)
	logger.With().Debug("global opinion", v, log.String("threshold", fmt.Sprint(threshold)))
	if float64(v[0]) > threshold {
		return support
	} else if float64(v[1]) > threshold {
		return against
	} else {
		return abstain
	}
}

type blockIDLayerTuple struct {
	types.BlockID
	types.LayerID
}

func (blt blockIDLayerTuple) layer() types.LayerID {
	return blt.LayerID
}

func (blt blockIDLayerTuple) id() types.BlockID {
	return blt.BlockID
}

// Opinion is a tuple of block and layer id and its opinions on other blocks.
type Opinion struct {
	BILT          blockIDLayerTuple
	BlocksOpinion map[types.BlockID]vec
}

type retriever interface {
	Retrieve(key []byte, v interface{}) (interface{}, error)
}
