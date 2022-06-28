package tortoise

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
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

type tortoiseBallot struct {
	id, base types.BallotID
	votes    votes
	abstain  map[types.LayerID]struct{}
	weight   util.Weight
}

func persistContextualValidity(logger log.Log,
	updater blockValidityUpdater,
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
			err = updater.UpdateBlockValidity(bid, lid, sign == support)
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
	reasonValidity       voteReason = "validity"
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

func maxLayer(i, j types.LayerID) types.LayerID {
	if i.After(j) {
		return i
	}
	return j
}
