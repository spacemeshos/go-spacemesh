package tortoisebeacon

import (
	"context"
	"fmt"
	"math/big"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

func (tb *TortoiseBeacon) calcVotes(ctx context.Context, epoch types.EpochID, round types.RoundID, coinFlip bool) (allVotes, error) {
	logger := tb.logger.WithContext(ctx).WithFields(epoch, round, log.Bool("weak_coin", coinFlip))
	tb.mu.Lock()
	defer tb.mu.Unlock()

	logger.With().Debug("calculating votes", log.String("vote_margins", fmt.Sprint(tb.votesMargin)))

	ownCurrentRoundVotes, err := tb.calcOwnCurrentRoundVotes(epoch, coinFlip)
	if err != nil {
		return allVotes{}, fmt.Errorf("calc own current round votes: %w", err)
	}

	logger.With().Debug("calculated votes for one round",
		log.String("for_votes", fmt.Sprint(ownCurrentRoundVotes.valid)),
		log.String("against_votes", fmt.Sprint(ownCurrentRoundVotes.invalid)))

	return ownCurrentRoundVotes, nil
}

func (tb *TortoiseBeacon) calcOwnCurrentRoundVotes(epoch types.EpochID, coinFlip bool) (allVotes, error) {
	ownCurrentRoundVotes := allVotes{
		valid:   make(proposalSet),
		invalid: make(proposalSet),
	}

	epochWeight, _, err := tb.atxDB.GetEpochWeight(epoch)
	if err != nil {
		return allVotes{}, fmt.Errorf("get epoch weight: %w", err)
	}

	positiveVotingThreshold := tb.votingThreshold(epochWeight)
	negativeThreshold := new(big.Int).Neg(positiveVotingThreshold)

	// TODO(nkryuchkov): should happen after weak coin for this round is calculated; consider calculating in two steps
	for vote, weightCount := range tb.votesMargin {
		switch {
		case weightCount.Cmp(positiveVotingThreshold) >= 0:
			ownCurrentRoundVotes.valid[vote] = struct{}{}
		case weightCount.Cmp(negativeThreshold) <= 0:
			ownCurrentRoundVotes.invalid[vote] = struct{}{}
		case coinFlip:
			ownCurrentRoundVotes.valid[vote] = struct{}{}
		case !coinFlip:
			ownCurrentRoundVotes.invalid[vote] = struct{}{}
		}
	}

	return ownCurrentRoundVotes, nil
}
