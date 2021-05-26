package tortoise

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type vec [2]int

var ( //correction vectors type
	// Opinion vectors
	support = vec{1, 0}
	against = vec{0, 1}
	abstain = vec{0, 0}
)

// Field returns a log field. Implements the LoggableField interface.
func (a vec) Field() log.Field {
	return log.String("vote_vector", fmt.Sprintf("%s (+%d, -%d)", a, a[0], a[1]))
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

func calculateOpinionWithThreshold(logger log.Log, v vec, layerSize int, theta uint8, delta float64) vec {
	threshold := float64(theta/100) * delta * float64(layerSize)
	netVote := float64(v[0] - v[1])
	logger.With().Debug("global opinion",
		v,
		log.String("threshold", fmt.Sprint(threshold)),
		log.String("net_vote", fmt.Sprint(netVote)))
	if netVote > threshold {
		// try net positive vote
		return support
	} else if netVote < -1*threshold {
		// try net negative vote
		return against
	} else {
		// neither threshold was crossed, so abstain
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

// Opinion is a tuple of block and layer id. It stores a block's opinion about many other blocks.
type Opinion struct {
	BlockAndLayer blockIDLayerTuple
	BlockOpinions map[types.BlockID]vec
}

type retriever interface {
	Retrieve(key []byte, v interface{}) (interface{}, error)
}
