package beacon

import (
	"fmt"
	"math/big"

	"github.com/spacemeshos/go-spacemesh/log"
)

func calcVotes(logger log.Log, theta *big.Float, s *state) (allVotes, proposalList) {
	logger.With().Debug("calculating votes", log.String("vote_margins", fmt.Sprint(s.votesMargin)))

	ownCurrentRoundVotes := allVotes{
		support: make(proposalSet),
		against: make(proposalSet),
	}

	positiveVotingThreshold := votingThreshold(theta, s.epochWeight)
	negativeThreshold := new(big.Int).Neg(positiveVotingThreshold)

	var undecided proposalList
	for vote, weightCount := range s.votesMargin {
		switch {
		case weightCount.Cmp(positiveVotingThreshold) >= 0:
			ownCurrentRoundVotes.support[vote] = struct{}{}
		case weightCount.Cmp(negativeThreshold) <= 0:
			ownCurrentRoundVotes.against[vote] = struct{}{}
		default:
			undecided = append(undecided, vote)
		}
	}
	logger.With().Debug("calculated votes for one round",
		log.String("for_votes", fmt.Sprint(ownCurrentRoundVotes.support)),
		log.String("against_votes", fmt.Sprint(ownCurrentRoundVotes.against)))

	return ownCurrentRoundVotes, undecided
}

func votingThreshold(theta *big.Float, epochWeight uint64) *big.Int {
	v, _ := new(big.Float).Mul(
		theta,
		new(big.Float).SetUint64(epochWeight),
	).Int(nil)

	return v
}

func tallyUndecided(votes *allVotes, undecided proposalList, coinFlip bool) {
	for _, vote := range undecided {
		if coinFlip {
			votes.support[vote] = struct{}{}
		} else {
			votes.against[vote] = struct{}{}
		}
	}
}
