package tortoise

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type vec [2]uint64

var (
	support     = vec{1, 0}
	against     = vec{0, 1}
	abstain     = vec{0, 0}
	errOverflow = errors.New("vector arithmetic overflow")
)

// Field returns a log field. Implements the LoggableField interface.
func (a vec) Field() log.Field {
	return log.String("vote_vector", fmt.Sprintf("%s (+%d, -%d)", a, a[0], a[1]))
}

func (a vec) Add(v vec) vec {
	a[0] += v[0]
	a[1] += v[1]
	// prevent overflow/wraparound
	if a[0] < v[0] || a[1] < v[1] {
		panic(errOverflow)
	}
	return a
}

func (a vec) Multiply(x uint64) vec {
	one := a[0] * x
	two := a[1] * x
	// prevent overflow/wraparound
	if x != 0 && (one/x != a[0] || two/x != a[1]) {
		panic(errOverflow)
	}
	return vec{one, two}
}

func (a vec) netVote() int64 {
	// prevent overflow/wraparound
	one := int64(a[0])
	two := int64(a[1])
	if one < 0 || two < 0 {
		panic(errOverflow)
	}
	return one - two
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
	threshold := float64(theta) / 100 * delta * float64(layerSize)
	netVote := float64(v.netVote())
	logger.With().Debug("threshold opinion",
		v,
		log.Int("theta", int(theta)),
		log.Int("layer_size", layerSize),
		log.String("delta", fmt.Sprint(delta)),
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

// Opinion is a tuple of block and layer id. It stores a block's opinion about many other blocks.
type Opinion map[types.LayerID]map[types.BlockID]vec

type retriever interface {
	Retrieve(key []byte, v interface{}) (interface{}, error)
}
