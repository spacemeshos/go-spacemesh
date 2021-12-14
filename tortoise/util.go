package tortoise

import (
	"fmt"
	"math/big"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
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

type votes map[types.BlockID]sign

func blockMapToArray(m map[types.BlockID]struct{}) []types.BlockID {
	arr := make([]types.BlockID, 0, len(m))
	for b := range m {
		arr = append(arr, b)
	}
	return arr
}

func weightFromInt64(value int64) weight {
	return weight{Rat: new(big.Rat).SetInt64(value)}
}

func weightFromUint64(value uint64) weight {
	return weight{Rat: new(big.Rat).SetUint64(value)}
}

func weightFromFloat64(value float64) weight {
	return weight{Rat: new(big.Rat).SetFloat64(value)}
}

// weight represents any weight in the tortoise.
type weight struct {
	*big.Rat
}

func (w weight) add(other weight) weight {
	w.Rat.Add(w.Rat, other.Rat)
	return w
}

func (w weight) sub(other weight) weight {
	w.Rat.Sub(w.Rat, other.Rat)
	return w
}

func (w weight) div(other weight) weight {
	w.Rat.Quo(w.Rat, other.Rat)
	return w
}

func (w weight) neg() weight {
	w.Rat.Neg(w.Rat)
	return w
}

func (w weight) copy() weight {
	other := weight{Rat: new(big.Rat)}
	other.Rat = other.Rat.Set(w.Rat)
	return other
}

func (w weight) fraction(frac *big.Rat) weight {
	w.Rat = w.Rat.Mul(w.Rat, frac)
	return w
}

func (w weight) cmp(other weight) sign {
	if w.Rat.Sign() == 1 && w.Rat.Cmp(other.Rat) == 1 {
		return support
	}
	if w.Rat.Sign() == -1 && new(big.Rat).Abs(w.Rat).Cmp(other.Rat) == 1 {
		return against
	}
	return abstain
}

func (w weight) String() string {
	if w.Rat.IsInt() {
		return w.Rat.Num().String()
	}
	return w.Rat.FloatString(2)
}

type tortoiseBallot struct {
	id, base types.BallotID
	votes    votes
	weight   weight
}

func persistContextualValidity(logger log.Log,
	bdp blockDataProvider,
	from, to types.LayerID,
	blocks map[types.LayerID][]types.BlockID,
	opinion votes,
) error {
	for lid := from.Add(1); !lid.After(to); lid = lid.Add(1) {
		for _, bid := range blocks[lid] {
			sign := opinion[bid]
			if sign == abstain {
				logger.With().Panic("bug: layer should not be verified if there is an undecided block", lid, bid)
			}
			if err := bdp.SaveContextualValidity(bid, lid, sign == support); err != nil {
				return fmt.Errorf("saving validity for %s: %w", bid, err)
			}
		}
	}
	return nil
}
