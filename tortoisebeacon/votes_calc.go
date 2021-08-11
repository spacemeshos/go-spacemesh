package tortoisebeacon

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

func (tb *TortoiseBeacon) calcVotes(epoch types.EpochID, round types.RoundID, coinFlip bool) (votesSetPair, error) {
	tb.votesMu.Lock()
	defer tb.votesMu.Unlock()

	tb.Log.With().Debug("Calculated first round votes",
		log.Uint32("epoch_id", uint32(epoch)),
		log.Uint32("round_id", uint32(round)),
		log.String("votesMargin", fmt.Sprint(tb.votesMargin)))

	ownFirstRoundVotes, err := tb.calcOwnFirstRoundVotes(epoch)
	if err != nil {
		return votesSetPair{}, fmt.Errorf("calc own first round votes: %w", err)
	}

	tb.Log.With().Debug("Calculated own first round votes",
		log.Uint32("epoch_id", uint32(epoch)),
		log.Uint32("round_id", uint32(round)),
		log.String("votesMargin", fmt.Sprint(tb.votesMargin)),
		log.String("ownFirstRoundVotes", fmt.Sprint(ownFirstRoundVotes)))

	if err := tb.calcVotesMargin(epoch, round); err != nil {
		return votesSetPair{}, fmt.Errorf("calc votes count: %w", err)
	}

	tb.Log.With().Debug("Calculated votes count",
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

// TODO: For every round excluding first round consider having a vector of opinions.
func (tb *TortoiseBeacon) calcOwnFirstRoundVotes(epoch types.EpochID) (votesSetPair, error) {
	ownFirstRoundsVotes := votesSetPair{
		ValidVotes:   make(hashSet),
		InvalidVotes: make(hashSet),
	}

	epochWeight, _, err := tb.atxDB.GetEpochWeight(epoch)
	if err != nil {
		return votesSetPair{}, fmt.Errorf("get epoch weight: %w", err)
	}

	votingThreshold := tb.votingThreshold(epochWeight)

	for vote, margin := range tb.votesMargin {
		switch {
		case int64(margin) >= votingThreshold:
			ownFirstRoundsVotes.ValidVotes[vote] = struct{}{}
		default:
			ownFirstRoundsVotes.InvalidVotes[vote] = struct{}{}
		}
	}

	if _, ok := tb.ownVotes[epoch]; !ok {
		tb.ownVotes[epoch] = make(map[types.RoundID]votesSetPair)
	}

	tb.ownVotes[epoch][firstRound] = ownFirstRoundsVotes

	return ownFirstRoundsVotes, nil
}

// TODO: Same calculation, do incremental part on receiving (vector of margins).
func (tb *TortoiseBeacon) calcVotesMargin(epoch types.EpochID, upToRound types.RoundID) error {
	for round := firstRound + 1; round < upToRound+firstRound; round++ {
		if tb.incomingVotes[round-firstRound] == nil {
			tb.incomingVotes[round-firstRound] = make(map[nodeID]votesSetPair)
		}

		thisRoundVotes := tb.incomingVotes[round-firstRound]

		for pk, votesList := range thisRoundVotes {
			voteWeight, err := tb.voteWeight(pk, epoch)
			if err != nil {
				return fmt.Errorf("vote weight: %w", err)
			}

			for vote := range votesList.ValidVotes {
				// TODO(nkryuchkov): handle overflow
				tb.votesMargin[vote] += int(voteWeight)
			}

			for vote := range votesList.InvalidVotes {
				// TODO(nkryuchkov): handle negative overflow
				tb.votesMargin[vote] -= int(voteWeight)
			}
		}
	}

	return nil
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

	if _, ok := tb.ownVotes[epoch]; !ok {
		tb.ownVotes[epoch] = make(map[types.RoundID]votesSetPair)
	}

	tb.ownVotes[epoch][round] = ownCurrentRoundVotes

	return ownCurrentRoundVotes, nil
}
