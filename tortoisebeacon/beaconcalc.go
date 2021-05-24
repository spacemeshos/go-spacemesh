package tortoisebeacon

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
)

func (tb *TortoiseBeacon) calcBeacon(epoch types.EpochID) {
	tb.Log.With().Info("Calculating beacon",
		log.Uint64("epoch_id", uint64(epoch)))

	allHashes := tb.calcTortoiseBeaconHashList(epoch)

	tb.Log.With().Info("Going to calculate tortoise beacon from this hash list",
		log.Uint64("epoch_id", uint64(epoch)),
		log.String("hashes", strings.Join(allHashes, ", ")))

	beacon := allHashes.Hash()

	tb.Log.With().Info("Calculated beacon",
		log.Uint64("epoch_id", uint64(epoch)),
		log.String("beacon", beacon.String()))

	events.ReportCalculatedTortoiseBeacon(epoch, beacon.String())

	tb.beaconsMu.Lock()
	tb.beacons[epoch] = beacon
	tb.beaconsMu.Unlock()
}

func (tb *TortoiseBeacon) calcTortoiseBeaconHashList(epoch types.EpochID) proposalList {
	allHashes := make(proposalList, 0)

	lastRound := epochRoundPair{
		EpochID: epoch,
		Round:   tb.lastPossibleRound(),
	}

	votes, ok := tb.ownVotes[lastRound]
	if !ok {
		// re-calculate votes
		votes = tb.calcVotes(epoch, lastRound.Round)
	}

	// output from VRF
	for vote := range votes.ValidVotes {
		allHashes = append(allHashes, vote)
	}

	tb.Log.With().Info("Tortoise beacon last round votes",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round", uint64(lastRound.Round)),
		log.String("votes", fmt.Sprint(votes)))

	sort.Slice(allHashes, func(i, j int) bool {
		return strings.Compare(allHashes[i], allHashes[j]) == -1
	})

	return allHashes
}
