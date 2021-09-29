package tortoisebeacon

import (
	"context"
	"strings"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
)

func (tb *TortoiseBeacon) calcBeacon(ctx context.Context, epoch types.EpochID, lastRoundVotes allVotes) error {
	logger := tb.logger.WithContext(ctx).WithFields(epoch)
	logger.Info("calculating beacon")

	allHashes := lastRoundVotes.valid.sort()
	allHashHexes := make([]string, len(allHashes))
	for i, h := range allHashes {
		allHashHexes[i] = types.BytesToHash([]byte(h)).ShortString()
	}
	logger.With().Debug("calculating tortoise beacon from this hash list",
		log.String("hashes", strings.Join(allHashHexes, ", ")))

	beacon := allHashes.hash()

	logger = logger.WithFields(log.String("beacon", beacon.ShortString()))
	logger.With().Info("calculated beacon", log.Int("num_hashes", len(allHashes)))

	events.ReportCalculatedTortoiseBeacon(epoch, beacon.ShortString())

	tb.setBeacon(epoch, beacon)

	logger.Debug("beacon updated for this epoch")
	return nil
}

func (tb *TortoiseBeacon) lastRound() types.RoundID {
	return tb.config.RoundsNumber - 1
}
