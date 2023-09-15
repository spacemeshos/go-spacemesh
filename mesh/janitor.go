package mesh

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh/metrics"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

func Prune(
	ctx context.Context,
	logger *zap.Logger,
	db sql.Executor,
	lc layerClock,
	safeDist uint32,
	interval time.Duration,
) {
	logger.With().Info("db pruning launched",
		zap.Uint32("dist", safeDist),
		zap.Duration("interval", interval),
	)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			oldest := lc.CurrentLayer() - types.LayerID(safeDist)
			t0 := time.Now()
			if err := proposals.Delete(db, oldest); err != nil {
				logger.Error("failed to delete proposals",
					zap.Stringer("lid", oldest),
					zap.Error(err),
				)
			}
			metrics.PruneProposalLatency.Observe(time.Since(t0).Seconds())
			t1 := time.Now()
			if err := certificates.DeleteCert(db, oldest); err != nil {
				logger.Error("failed to delete certificates",
					zap.Stringer("lid", oldest),
					zap.Error(err),
				)
			}
			metrics.PruneCertLatency.Observe(time.Since(t1).Seconds())
			t2 := time.Now()
			if err := transactions.DeleteProposalTxs(db, oldest); err != nil {
				logger.Error("failed to delete proposal tx mapping",
					zap.Stringer("lid", oldest),
					zap.Error(err),
				)
			}
			metrics.PrunePropTxLatency.Observe(time.Since(t2).Seconds())
		}
	}
}

func ExtractActiveSet(db sql.Executor) error {
	latest, err := ballots.LatestLayer(db)
	if err != nil {
		return fmt.Errorf("extract get latest: %w", err)
	}
	extracted := 0
	unique := 0
	log.With().Info("extracting ballots active sets",
		log.Uint32("from", types.EpochID(2).FirstLayer().Uint32()),
		log.Uint32("to", latest.Uint32()),
	)
	for lid := types.EpochID(2).FirstLayer(); lid <= latest; lid++ {
		blts, err := ballots.Layer(db, lid)
		if err != nil {
			return fmt.Errorf("extract layer %d: %w", lid, err)
		}
		for _, b := range blts {
			if b.EpochData == nil {
				continue
			}
			if len(b.ActiveSet) == 0 {
				continue
			}
			if err := activesets.Add(db, b.EpochData.ActiveSetHash, &types.EpochActiveSet{
				Epoch: b.Layer.GetEpoch(),
				Set:   b.ActiveSet,
			}); err != nil && !errors.Is(err, sql.ErrObjectExists) {
				return fmt.Errorf("add active set %s (%s): %w", b.ID().String(), b.EpochData.ActiveSetHash.ShortString(), err)
			} else if err == nil {
				unique++
			}
			b.ActiveSet = nil
			if err := ballots.UpdateBlob(db, b.ID(), codec.MustEncode(b)); err != nil {
				return fmt.Errorf("update ballot %s: %w", b.ID().String(), err)
			}
			extracted++
		}
	}
	log.With().Info("extracted active sets from ballots",
		log.Int("num", extracted),
		log.Int("unique", unique),
	)
	return nil
}
