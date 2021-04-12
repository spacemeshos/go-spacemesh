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

	stringHashes := make([]string, 0, len(allHashes))
	for _, hash := range allHashes {
		stringHashes = append(stringHashes, hash.String())
	}

	tb.Log.With().Info("Going to calculate tortoise beacon from this hash list",
		log.Uint64("epoch_id", uint64(epoch)),
		log.String("hashes", strings.Join(stringHashes, ", ")))

	beacon := allHashes.Hash()

	tb.Log.With().Info("Calculated beacon",
		log.Uint64("epoch_id", uint64(epoch)),
		log.String("beacon", beacon.String()))

	events.ReportCalculatedTortoiseBeacon(epoch, beacon.String())

	tb.beaconsMu.Lock()

	tb.beacons[epoch] = beacon
	close(tb.beaconsReady[epoch]) // indicate that value is ready

	tb.beaconsReady[epoch+1] = make(chan struct{}) // get the next epoch ready

	tb.beaconsMu.Unlock()
}

func (tb *TortoiseBeacon) calcTortoiseBeaconHashList(epoch types.EpochID) hashList {
	allHashes := make(hashList, 0)

	lastRound := epochRoundPair{
		EpochID: epoch,
		Round:   tb.lastRound(),
	}

	votes, ok := tb.ownVotes[lastRound]
	if !ok {
		_, _ = tb.calcVotesDelta(epoch, lastRound.Round)
		if votes, ok = tb.ownVotes[lastRound]; !ok {
			panic("calcVotesDelta didn't calculate own votes")
		}
	}

	for vote := range votes.VotesFor {
		allHashes = append(allHashes, vote)
	}

	tb.Log.With().Info("Tortoise beacon last round votes",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round", uint64(lastRound.Round)),
		log.String("votes", fmt.Sprint(votes)))

	sort.Slice(allHashes, func(i, j int) bool {
		return strings.Compare(allHashes[i].String(), allHashes[j].String()) == -1
	})

	return allHashes
}
