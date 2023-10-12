package beacon

import (
	"encoding/hex"
	"math/big"

	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/log"
)

func calcVotes(logger log.Log, theta *big.Float, s *state) (allVotes, proposalList) {
	logger.With().Debug("calculating votes", log.Object("vote_margins", zapcore.ObjectMarshalerFunc(
		func(enc zapcore.ObjectEncoder) error {
			for vote, margin := range s.votesMargin {
				enc.AddString(hex.EncodeToString(vote[:]), margin.String())
			}
			return nil
		})))

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
		log.Array("for_votes", ownCurrentRoundVotes.support),
		log.Array("against_votes", ownCurrentRoundVotes.against),
	)

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
