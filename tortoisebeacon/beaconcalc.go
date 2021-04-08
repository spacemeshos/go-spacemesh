package tortoisebeacon

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spacemeshos/sha256-simd"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
)

func (tb *TortoiseBeacon) calcBeacon(epoch types.EpochID) {
	tb.Log.With().Info("Calculating beacon",
		log.Uint64("epoch", uint64(epoch)))

	allHashes := tb.calcTortoiseBeaconHashList(epoch)

	stringHashes := make([]string, 0, len(allHashes))
	for _, hash := range allHashes {
		stringHashes = append(stringHashes, hash.String())
	}

	tb.Log.With().Info(fmt.Sprintf("Going to calculate tortoise beacon from this hash list epoch %v", epoch),
		log.Uint64("epoch_id", uint64(epoch)),
		log.String("hashes", strings.Join(stringHashes, ", ")))

	beacon := tb.calcBeaconFromHashList(allHashes)

	tb.Log.With().Info("Calculated beacon",
		log.Uint64("epoch", uint64(epoch)),
		log.String("beacon", beacon.String()))

	events.ReportCalculatedTortoiseBeacon(epoch, beacon.String())

	tb.beaconsMu.Lock()

	tb.beacons[epoch] = beacon
	close(tb.beaconsReady[epoch]) // indicate that value is ready

	tb.beaconsReady[epoch+1] = make(chan struct{}) // get the next epoch ready

	tb.beaconsMu.Unlock()
}

func (tb *TortoiseBeacon) calcBeaconFromHashList(hashes hashList) types.Hash32 {
	hasher := sha256.New()

	for _, hash := range hashes {
		if _, err := hasher.Write(hash.Bytes()); err != nil {
			panic("should not happen") // an error is never returned: https://golang.org/pkg/hash/#Hash
		}
	}

	var beacon types.Hash32

	hasher.Sum(beacon[:0])

	return beacon
}

func (tb *TortoiseBeacon) calcTortoiseBeaconHashList(epoch types.EpochID) hashList {
	allHashes := make(hashList, 0)

	for round := firstRound; round <= types.RoundID(tb.config.RoundsNumber); round++ {
		epochRound := epochRoundPair{
			EpochID: epoch,
			Round:   round,
		}

		stringHashes := make([]string, 0)

		if roundVotes, ok := tb.votesCountCache[epochRound]; ok {
			for hash, count := range roundVotes {
				if count >= tb.threshold() {
					allHashes = append(allHashes, hash)
					stringHashes = append(stringHashes, hash.String())
				}
			}

			tb.Log.With().Info(fmt.Sprintf("Tortoise beacon round votes epoch %v round %v: %+v", epoch, round, roundVotes),
				log.Uint64("epoch_id", uint64(epoch)),
				log.Uint64("round", uint64(round)))
		}

		tb.Log.With().Info(fmt.Sprintf("Tortoise beacon hashes epoch %v round %v", epoch, round),
			log.Uint64("epoch_id", uint64(epoch)),
			log.Uint64("round", uint64(round)),
			log.String("hashes", strings.Join(stringHashes, ", ")))
	}

	sort.Slice(allHashes, func(i, j int) bool {
		return strings.Compare(allHashes[i].String(), allHashes[j].String()) == -1
	})

	return allHashes
}
