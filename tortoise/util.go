package tortoise

import (
	"math/big"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

const (
	support sign = 1
	against sign = -1
	abstain sign = 0
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

// Opinion is opinions on other blocks.
type Opinion map[types.BlockID]sign

func blockMapToArray(m map[types.BlockID]struct{}) []types.BlockID {
	arr := make([]types.BlockID, 0, len(m))
	for b := range m {
		arr = append(arr, b)
	}
	return arr
}

func weightFromInt64(value int64) weight {
	return weight{Float: new(big.Float).SetInt64(value)}
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

func (w weight) add(other weight) weight {
	w.Float.Add(w.Float, other.Float)
	return w
}

func (w weight) sub(other weight) weight {
	w.Float.Sub(w.Float, other.Float)
	return w
}

func (w weight) div(other weight) weight {
	w.Float.Quo(w.Float, other.Float)
	return w
}

func (w weight) neg() weight {
	w.Float.Neg(w.Float)
	return w
}

func (w weight) copy() weight {
	other := weight{Float: new(big.Float)}
	other.Float = other.Float.Copy(w.Float)
	return other
}

func (w weight) fraction(frac *big.Rat) weight {
	threshold := new(big.Float).SetInt(frac.Num())
	threshold.Mul(threshold, w.Float)
	threshold.Quo(threshold, new(big.Float).SetInt(frac.Denom()))
	w.Float = threshold
	return w
}

func (w weight) cmp(other weight) sign {
	if w.Float.Sign() == 1 && w.Float.Cmp(other.Float) == 1 {
		return support
	}
	if w.Float.Sign() == -1 && new(big.Float).Abs(w.Float).Cmp(other.Float) == 1 {
		return against
	}
	return abstain
}

func (w weight) String() string {
	return w.Float.String()
}

type tortoiseBallot struct {
	id, base types.BallotID
	votes    Opinion
	weight   weight
}
