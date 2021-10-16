package tortoisebeacon

import (
	"context"
	"fmt"
	"math/big"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

func (tb *TortoiseBeacon) calcVotes(ctx context.Context, epoch types.EpochID, round types.RoundID) (allVotes, []string, error) {
	logger := tb.logger.WithContext(ctx).WithFields(epoch, round)
	tb.mu.Lock()
	defer tb.mu.Unlock()

	logger.With().Debug("calculating votes", log.String("vote_margins", fmt.Sprint(tb.votesMargin)))

	ownCurrentRoundVotes, undecided, err := tb.calcOwnCurrentRoundVotes()
	if err != nil {
		return allVotes{}, nil, fmt.Errorf("calc own current round votes: %w", err)
	}

	logger.With().Debug("calculated votes for one round",
		log.String("for_votes", fmt.Sprint(ownCurrentRoundVotes.valid)),
		log.String("against_votes", fmt.Sprint(ownCurrentRoundVotes.invalid)))

	return ownCurrentRoundVotes, undecided, nil
}

func (tb *TortoiseBeacon) calcOwnCurrentRoundVotes() (allVotes, []string, error) {
	ownCurrentRoundVotes := allVotes{
		valid:   make(proposalSet),
		invalid: make(proposalSet),
	}

	positiveVotingThreshold := tb.votingThreshold(tb.epochWeight)
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
