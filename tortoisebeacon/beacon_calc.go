package tortoisebeacon

import (
	"fmt"
	"strings"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
)

func (tb *TortoiseBeacon) calcBeacon(epoch types.EpochID, lastRoundVotes allVotes) error {
	tb.Log.With().Info("calculating beacon", log.Uint32("epoch_id", uint32(epoch)))

	allHashes := lastRoundVotes.valid.sort()

	tb.Log.With().Debug("calculating tortoise beacon from this hash list",
		log.Uint32("epoch_id", uint32(epoch)),
		log.String("hashes", strings.Join(allHashes, ", ")))

	beacon := allHashes.hash()

	tb.Log.With().Info("calculated beacon", log.Uint32("epoch_id", uint32(epoch)),
		log.String("beacon", beacon.String()))

	events.ReportCalculatedTortoiseBeacon(epoch, beacon.String())

	tb.beaconsMu.Lock()
	tb.beacons[epoch] = beacon
	tb.beaconsMu.Unlock()

	if tb.tortoiseBeaconDB != nil {
		tb.Log.With().Info("writing beacon to database",
			log.Uint32("epoch_id", uint32(epoch)),
			log.String("beacon", beacon.String()))

		if err := tb.tortoiseBeaconDB.SetTortoiseBeacon(epoch, beacon); err != nil {
			tb.Log.With().Error("failed to write tortoise beacon to database",
				log.Uint32("epoch_id", uint32(epoch)),
				log.String("beacon", beacon.String()))

			return fmt.Errorf("write tortoise beacon to DB: %w", err)
		}
	}

	tb.Log.With().Debug("beacon updated for this epoch",
		log.Uint32("epoch_id", uint32(epoch)),
		log.String("beacon", beacon.String()))

	return nil
}

func (tb *TortoiseBeacon) lastRound() types.RoundID {
	return tb.config.RoundsNumber - 1 + firstRound
}
