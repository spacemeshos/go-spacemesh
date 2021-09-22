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

var hundred = big.NewInt(100) // just to avoid computing it below every time

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
	threshold := big.NewInt(int64(theta))
	threshold.Mul(threshold, big.NewInt(int64(delta)))
	threshold.Mul(threshold, big.NewInt(int64(layerSize)))

	logger.With().Debug("threshold opinion",
		v,
		log.Int("theta", int(theta)),
		log.Int("layer_size", layerSize),
		log.Uint32("delta", delta),
		log.String("threshold", threshold.String()))

	greater := func(val uint64) bool {
		v := new(big.Int).SetUint64(val)
		return v.Mul(v, hundred).Cmp(threshold) == 1
	}
	if v.Support > v.Against && greater(v.Support-v.Against) {
		return support
	} else if v.Against > v.Support && greater(v.Against-v.Support) {
		return against
	}
	return abstain
}

// Opinion is opinions on other blocks.
type Opinion map[types.BlockID]vec
