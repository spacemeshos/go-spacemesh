package tortoise

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

var errOverflow = errors.New("vector overflow")

var (
	// correction vectors type
	// Opinion vectors
	support = vec{Support: 1, Against: 0}
	against = vec{Support: 0, Against: 1}
	abstain = vec{Support: 0, Against: 0}
)

type vec struct {
	Support, Against uint64
	Flushed          bool
}

// Field returns a log field. Implements the LoggableField interface.
func (a vec) Field() log.Field {
	return log.String("vote_vector", fmt.Sprintf("(+%d, -%d)", a.Support, a.Against))
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

func calculateOpinionWithThreshold(logger log.Log, v vec, layerSize int, theta uint8, delta uint32) vec {
	threshold := new(big.Float).SetUint64(uint64(theta))
	threshold = threshold.Quo(threshold, big.NewFloat(100))
	threshold = threshold.Mul(threshold, new(big.Float).SetUint64(uint64(delta)))
	threshold = threshold.Mul(threshold, new(big.Float).SetUint64(uint64(layerSize)))

	logger.With().Debug("threshold opinion",
		v,
		log.Int("theta", int(theta)),
		log.Int("layer_size", layerSize),
		log.Uint32("delta", delta),
		log.String("threshold", threshold.String()))

	if v.Support > v.Against && big.NewFloat(float64(v.Support-v.Against)).Cmp(threshold) > 0 {
		return support
	} else if v.Against > v.Support && big.NewFloat(float64(v.Against-v.Support)).Cmp(threshold) > 0 {
		return against
	}
	return abstain
}

// Opinion is opinions on other blocks.
type Opinion map[types.BlockID]vec
