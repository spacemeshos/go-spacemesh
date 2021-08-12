package tortoisebeacon

import (
	"fmt"
	"strings"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
)

func (tb *TortoiseBeacon) calcBeacon(epoch types.EpochID, lastRoundVotes allVotes) error {
	tb.Log.With().Info("Calculating beacon",
		log.Uint32("epoch_id", uint32(epoch)))

	allHashes := lastRoundVotes.valid.sort()

	tb.Log.With().Debug("Going to calculate tortoise beacon from this hash list",
		log.Uint32("epoch_id", uint32(epoch)),
		log.String("hashes", strings.Join(allHashes, ", ")))

	beacon := allHashes.hash()

	tb.Log.With().Info("Calculated beacon",
		log.Uint32("epoch_id", uint32(epoch)),
		log.String("beacon", beacon.String()))

	events.ReportCalculatedTortoiseBeacon(epoch, beacon.String())

	tb.beaconsMu.Lock()
	tb.beacons[epoch] = beacon
	tb.beaconsMu.Unlock()

	if tb.tortoiseBeaconDB != nil {
		tb.Log.With().Info("Writing beacon to database",
			log.Uint32("epoch_id", uint32(epoch)),
			log.String("beacon", beacon.String()))

		if err := tb.tortoiseBeaconDB.SetTortoiseBeacon(epoch, beacon); err != nil {
			tb.Log.With().Error("Failed to write tortoise beacon to DB",
				log.Uint32("epoch_id", uint32(epoch)),
				log.String("beacon", beacon.String()))

			return fmt.Errorf("write tortoise beacon to DB: %w", err)
		}
	}

	tb.Log.With().Debug("Beacon updated for this epoch",
		log.Uint32("epoch_id", uint32(epoch)),
		log.String("beacon", beacon.String()))

	return nil
}

func (tb *TortoiseBeacon) lastRound() types.RoundID {
	return tb.config.RoundsNumber - 1 + firstRound
}
