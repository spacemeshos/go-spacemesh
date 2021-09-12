package tortoisebeacon

import (
	"context"
	"fmt"
	"strings"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
)

func (tb *TortoiseBeacon) calcBeacon(ctx context.Context, epoch types.EpochID, lastRoundVotes allVotes) error {
	tb.Log.WithContext(ctx).With().Info("calculating beacon", log.Uint32("epoch_id", uint32(epoch)))

	allHashes := lastRoundVotes.valid.sort()

	tb.Log.WithContext(ctx).With().Debug("calculating tortoise beacon from this hash list",
		log.Uint32("epoch_id", uint32(epoch)),
		log.String("hashes", strings.Join(allHashes, ", ")))

	beacon := allHashes.hash()

	tb.Log.WithContext(ctx).With().Info("calculated beacon", log.Uint32("epoch_id", uint32(epoch)),
		log.String("beacon", beacon.ShortString()))

	events.ReportCalculatedTortoiseBeacon(epoch, beacon.ShortString())

	tb.setBeacon(epoch, beacon)
	if tb.tortoiseBeaconDB != nil {
		tb.Log.WithContext(ctx).With().Info("writing beacon to database",
			log.Uint32("epoch_id", uint32(epoch)),
			log.String("beacon", beacon.ShortString()))

		if err := tb.tortoiseBeaconDB.SetTortoiseBeacon(epoch, beacon); err != nil {
			tb.Log.WithContext(ctx).With().Error("failed to write tortoise beacon to database",
				log.Uint32("epoch_id", uint32(epoch)),
				log.String("beacon", beacon.ShortString()))

			return fmt.Errorf("write tortoise beacon to DB: %w", err)
		}
	}

	tb.Log.WithContext(ctx).With().Debug("beacon updated for this epoch",
		log.Uint32("epoch_id", uint32(epoch)),
		log.String("beacon", beacon.ShortString()))

	return nil
}

func (tb *TortoiseBeacon) lastRound() types.RoundID {
	return tb.config.RoundsNumber - 1
}
