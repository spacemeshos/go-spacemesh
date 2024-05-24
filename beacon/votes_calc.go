package beacon

import (
	"encoding/hex"
	"math/big"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func calcVotes(logger *zap.Logger, theta *big.Float, s *state) (allVotes, proposalList) {
	logger.Debug("calculating votes",
		zap.Object("vote_margins", zapcore.ObjectMarshalerFunc(func(enc zapcore.ObjectEncoder) error {
			for vote, margin := range s.votesMargin {
				enc.AddString(hex.EncodeToString(vote[:]), margin.String())
			}
			return nil
		})),
	)

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
	logger.Debug("calculated votes for one round",
		zap.Array("for_votes", ownCurrentRoundVotes.support),
		zap.Array("against_votes", ownCurrentRoundVotes.against),
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
