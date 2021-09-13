package tortoisebeacon

import (
	"fmt"
	"math/big"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

func (tb *TortoiseBeacon) calcVotes(epoch types.EpochID, round types.RoundID) (allVotes, []string, error) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.Log.With().Debug("Calculating votes",
		log.Uint32("epoch_id", uint32(epoch)),
		log.Uint32("round_id", uint32(round)),
		log.String("votesMargin", fmt.Sprint(tb.votesMargin)))

	ownCurrentRoundVotes, undecided, err := tb.calcOwnCurrentRoundVotes(epoch)
	if err != nil {
		return allVotes{}, nil, fmt.Errorf("calc own current round votes: %w", err)
	}

	tb.Log.With().Debug("Calculated votes for one round",
		log.Uint32("epoch_id", uint32(epoch)),
		log.Uint32("round_id", uint32(round)),
		log.String("for", fmt.Sprint(ownCurrentRoundVotes.valid)),
		log.String("against", fmt.Sprint(ownCurrentRoundVotes.invalid)))

	return ownCurrentRoundVotes, undecided, nil
}

func (tb *TortoiseBeacon) calcOwnCurrentRoundVotes(epoch types.EpochID) (allVotes, []string, error) {
	ownCurrentRoundVotes := allVotes{
		valid:   make(proposalSet),
		invalid: make(proposalSet),
	}

	epochWeight, _, err := tb.atxDB.GetEpochWeight(epoch)
	if err != nil {
		return allVotes{}, nil, fmt.Errorf("get epoch weight: %w", err)
	}

	positiveVotingThreshold := tb.votingThreshold(epochWeight)
	negativeThreshold := new(big.Int).Neg(positiveVotingThreshold)

	var undecided []string
	for vote, weightCount := range tb.votesMargin {
		switch {
		case weightCount.Cmp(positiveVotingThreshold) >= 0:
			ownCurrentRoundVotes.valid[vote] = struct{}{}
		case weightCount.Cmp(negativeThreshold) <= 0:
			ownCurrentRoundVotes.invalid[vote] = struct{}{}
		default:
			undecided = append(undecided, vote)
		}
	}
	return ownCurrentRoundVotes, undecided, nil
}

func tallyUndecided(votes *allVotes, undecided []string, coinFlip bool) {
	for _, vote := range undecided {
		if coinFlip {
			votes.valid[vote] = struct{}{}
		} else {
			votes.invalid[vote] = struct{}{}
		}
	}
}
