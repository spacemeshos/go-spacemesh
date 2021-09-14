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
	logger := tb.logger.WithContext(ctx).WithFields(epoch)
	logger.Info("calculating beacon")

	allHashes := lastRoundVotes.valid.sort()
	logger.With().Debug("calculating tortoise beacon from this hash list",
		log.String("hashes", strings.Join(allHashes, ", ")))

	beacon := allHashes.hash()

	logger = logger.WithFields(log.String("beacon", beacon.ShortString()))
	logger.With().Info("calculated beacon", log.Int("num_hashes", len(allHashes)))

	events.ReportCalculatedTortoiseBeacon(epoch, beacon.ShortString())

	tb.setBeacon(epoch, beacon)
	if tb.tortoiseBeaconDB != nil {
		logger.Info("writing beacon to database")
		if err := tb.tortoiseBeaconDB.SetTortoiseBeacon(epoch, beacon); err != nil {
			logger.With().Error("failed to write tortoise beacon to database", log.Err(err))
			return fmt.Errorf("write tortoise beacon to DB: %w", err)
		}
	}

	logger.Debug("beacon updated for this epoch")
	return nil
}

func (tb *TortoiseBeacon) lastRound() types.RoundID {
	return tb.config.RoundsNumber - 1
}
