package tortoise

import (
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type vec struct {
	Support, Against uint64
	Flushed          bool
}

const (
	globalThreshold = 0.6
)

var ( //correction vectors type
	// Opinion vectors
	support = vec{Support: 1, Against: 0}
	against = vec{Support: 0, Against: 1}
	abstain = vec{Support: 0, Against: 0}
)

var errOverflow = errors.New("vector overflow")

// Field returns a log field. Implements the LoggableField interface.
func (a vec) Field() log.Field {
	return log.String("vote_vector", fmt.Sprintf("(+%d, -%d)", a.Support, a.Against))
}

func (a vec) netVote() int64 {
	// prevent overflow/wraparound
	one := int64(a.Support)
	two := int64(a.Against)
	if one < 0 || two < 0 {
		panic(errOverflow)
	}
	return one - two
}

func (a vec) Add(v vec) vec {
	a.Support += v.Support
	a.Against += v.Against
	a.Flushed = false
	if a.Support < v.Support || a.Against < v.Against {
		panic(errOverflow)
	}
	return a
}

func (a vec) Multiply(x uint64) vec {
	one := a.Support * x
	two := a.Against * x
	if x != 0 && (one/x != a.Support || two/x != a.Against) {
		panic(errOverflow)
	}
	a.Flushed = false
	a.Support = one
	a.Against = two
	return a
}

func simplifyVote(v vec) vec {
	if v.Support > v.Against {
		return support
	}

	if v.Against > v.Support {
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

// Opinion is opinions on other blocks.
type Opinion map[types.BlockID]vec
