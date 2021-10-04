package tortoise

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

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

// Some methods of *big.Rat change its internal data without mutexes, which causes data races.
var thetaMu sync.Mutex

// calculateOpinionWithThreshold computes opinion vector (support, against, abstain) based on the vote weight
// and theta, layer size and delta
func calculateOpinionWithThreshold(logger log.Log, v vec, theta *big.Rat, layerSize uint32, delta uint32) vec {
	thetaMu.Lock()
	defer thetaMu.Unlock()

	threshold := new(big.Int).Set(theta.Num())
	threshold.
		Mul(threshold, big.NewInt(int64(delta))).
		Mul(threshold, big.NewInt(int64(layerSize)))

	logger.With().Debug("threshold opinion",
		v,
		log.String("theta", theta.String()),
		log.Uint32("layer_size", layerSize),
		log.Uint32("delta", delta),
		log.String("threshold", threshold.String()))

	exceedsThreshold := func(val uint64) bool {
		v := new(big.Int).SetUint64(val)
		return v.Mul(v, theta.Denom()).Cmp(threshold) == 1
	}
	if v.Support > v.Against && exceedsThreshold(v.Support-v.Against) {
		return support
	} else if v.Against > v.Support && exceedsThreshold(v.Against-v.Support) {
		return against
	}
	return abstain
}

// Opinion is opinions on other blocks.
type Opinion map[types.BlockID]vec
