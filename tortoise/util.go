package tortoise

import (
	"fmt"
	"math/big"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	verifyingTortoise = "verifying"
	fullTortoise      = "full"
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

func (w weight) isNil() bool {
	return w.Rat == nil
}

type tortoiseBallot struct {
	id, base types.BallotID
	votes    votes
	abstain  map[types.LayerID]struct{}
	weight   weight
}

func persistContextualValidity(logger log.Log,
	bdp blockDataProvider,
	from, to types.LayerID,
	blocks map[types.LayerID][]types.BlockID,
	opinion votes,
) error {
	var err error
	iterateLayers(from.Add(1), to, func(lid types.LayerID) bool {
		for _, bid := range blocks[lid] {
			sign := opinion[bid]
			if sign == abstain {
				logger.With().Panic("bug: layer should not be verified if there is an undecided block", lid, bid)
			}
			err = bdp.SaveContextualValidity(bid, lid, sign == support)
			if err != nil {
				err = fmt.Errorf("saving validity for %s: %w", bid, err)
				return false
			}
		}
		return true
	})
	return err
}

func iterateLayers(from, to types.LayerID, callback func(types.LayerID) bool) {
	for lid := from; !lid.After(to); lid = lid.Add(1) {
		if !callback(lid) {
			return
		}
	}
}

type voteReason string

func (v voteReason) String() string {
	return string(v)
}

const (
	reasonHareOutput     voteReason = "hare"
	reasonValiditiy      voteReason = "validity"
	reasonLocalThreshold voteReason = "local_threshold"
	reasonCoinflip       voteReason = "coinflip"
)

// lsb is a mode, second bit is rerun
// examples (lsb is on the right, prefix with 6 bits is droppped)
// 10 - rerun in verifying
// 00 - live tortoise in verifying
// 11 - rerun in full
// 01 - live tortoise in full.
type mode [2]bool

func (m mode) String() string {
	humanize := "verifying"
	if m.isFull() {
		humanize = "full"
	}
	if m.isRerun() {
		return "rerun_" + humanize
	}
	return humanize
}

func (m mode) toggleRerun() mode {
	m[1] = !m[1]
	return m
}

func (m mode) isRerun() bool {
	return m[1]
}

func (m mode) toggleMode() mode {
	m[0] = !m[0]
	return m
}

func (m mode) isVerifying() bool {
	return !m[0]
}

func (m mode) isFull() bool {
	return m[0]
}
