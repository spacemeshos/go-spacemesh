package tortoise

import (
	"math/big"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	support = 1
	against = -1
	abstain = 0
)

type sign int8

func (a sign) String() string {
	switch a {
	case 1:
		return "support"
	case -1:
		return "against"
	case 0:
		return "abstain"
	default:
		panic("sign should be 0/-1/1")
	}
}

// Some methods of *big.Rat change its internal data without mutexes, which causes data races.
//
// TODO(dshulyak) big.Rat is being passed without copy somewhere in the initialization.
// copy big.Rat to remove this mutex.
var thetaMu sync.Mutex

// calculateOpinionWithThreshold computes opinion vector (support, against, abstain) based on the vote weight
// theta, and expected vote weight.
func calculateOpinionWithThreshold(logger log.Log, vote *big.Float, theta *big.Rat, weight *big.Float) sign {
	thetaMu.Lock()
	defer thetaMu.Unlock()

	threshold := new(big.Float).SetInt(theta.Num())
	threshold.Mul(threshold, weight)
	threshold.Quo(threshold, new(big.Float).SetInt(theta.Denom()))

	logger.With().Debug("threshold opinion",
		log.Stringer("vote", vote),
		log.Stringer("theta", theta),
		log.Stringer("expected_weight", weight),
		log.Stringer("threshold", threshold),
	)

	if vote.Sign() == 1 && vote.Cmp(threshold) == 1 {
		return support
	}
	if vote.Sign() == -1 && vote.Abs(vote).Cmp(threshold) == 1 {
		return against
	}
	return abstain
}

// Opinion is opinions on other blocks.
type Opinion map[types.BlockID]sign

func blockMapToArray(m map[types.BlockID]struct{}) []types.BlockID {
	arr := make([]types.BlockID, 0, len(m))
	for b := range m {
		arr = append(arr, b)
	}
	return arr
}

func weightFromUint64(value uint64) weight {
	return weight{Float: new(big.Float).SetUint64(value)}
}

// weight represents any weight in the tortoise.
//
// TODO(dshulyak) needs to be replaced with fixed float
// both for performance and safety.
type weight struct {
	*big.Float
}

func (w *weight) ensure() {
	if w.Float == nil {
		w.Float = new(big.Float)
	}
}

func (w weight) add(other weight) weight {
	w.ensure()
	w.Float.Add(w.Float, other.Float)
	return w
}

func (w weight) sub(other weight) weight {
	w.ensure()
	w.Float.Sub(w.Float, other.Float)
	return w
}

func (w weight) div(other weight) weight {
	w.ensure()
	w.Float.Quo(w.Float, other.Float)
	return w
}

func (w weight) neg() weight {
	w.ensure()
	w.Float.Neg(w.Float)
	return w
}

func (w weight) copy() weight {
	w.ensure()
	other := weight{Float: new(big.Float)}
	other.Float = other.Float.Copy(w.Float)
	return other
}

func (w weight) fraction(frac *big.Rat) weight {
	w.ensure()
	threshold := new(big.Float).SetInt(frac.Num())
	threshold.Mul(threshold, w.Float)
	threshold.Quo(threshold, new(big.Float).SetInt(frac.Denom()))
	w.Float = threshold
	return w
}

func (w weight) cmp(other weight) sign {
	w.ensure()
	if w.Float.Sign() == 1 && w.Float.Cmp(other.Float) == 1 {
		return support
	}
	if w.Float.Sign() == -1 && new(big.Float).Abs(w.Float).Cmp(other.Float) == 1 {
		return against
	}
	return abstain
}

func (w weight) String() string {
	w.ensure()
	return w.Float.String()
}

type tortoiseBallot struct {
	id, base types.BallotID
	votes    Opinion
	weight   weight
}
