package tortoisebeacon

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

func (tb *TortoiseBeacon) calcVotes(epoch types.EpochID, round types.RoundID, coinFlip bool) (votesSetPair, error) {
	tb.votesMu.Lock()
	defer tb.votesMu.Unlock()

	tb.Log.With().Debug("Calculating votes",
		log.Uint32("epoch_id", uint32(epoch)),
		log.Uint32("round_id", uint32(round)),
		log.String("votesMargin", fmt.Sprint(tb.votesMargin)))

	ownCurrentRoundVotes, err := tb.calcOwnCurrentRoundVotes(epoch, round, coinFlip)
	if err != nil {
		return votesSetPair{}, fmt.Errorf("calc own current round votes: %w", err)
	}

	tb.Log.With().Debug("Calculated votes for one round",
		log.Uint32("epoch_id", uint32(epoch)),
		log.Uint32("round_id", uint32(round)),
		log.String("for", fmt.Sprint(ownCurrentRoundVotes.ValidVotes)),
		log.String("against", fmt.Sprint(ownCurrentRoundVotes.InvalidVotes)))

	return ownCurrentRoundVotes, nil
}

func (tb *TortoiseBeacon) calcOwnCurrentRoundVotes(epoch types.EpochID, round types.RoundID, coinflip bool) (votesSetPair, error) {
	ownCurrentRoundVotes := votesSetPair{
		ValidVotes:   make(hashSet),
		InvalidVotes: make(hashSet),
	}

	epochWeight, _, err := tb.atxDB.GetEpochWeight(epoch)
	if err != nil {
		return votesSetPair{}, fmt.Errorf("get epoch weight: %w", err)
	}

	votingThreshold := tb.votingThreshold(epochWeight)

	// TODO(nkryuchkov): should happen after weak coin for this round is calculated; consider calculating in two steps
	for vote, weightCount := range tb.votesMargin {
		wc := int64(weightCount)

		switch {
		case wc >= votingThreshold:
			ownCurrentRoundVotes.ValidVotes[vote] = struct{}{}
		case wc <= -votingThreshold:
			ownCurrentRoundVotes.InvalidVotes[vote] = struct{}{}
		case coinflip:
			ownCurrentRoundVotes.ValidVotes[vote] = struct{}{}
		case !coinflip:
			ownCurrentRoundVotes.InvalidVotes[vote] = struct{}{}
		}
	}

	if round == tb.lastRound() {
		tb.ownLastRoundVotes = ownCurrentRoundVotes
	}

	return ownCurrentRoundVotes, nil
}
