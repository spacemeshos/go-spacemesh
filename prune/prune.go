package prune

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
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
			if err := proposals.DeleteBefore(db, oldest); err != nil {
				logger.Error("failed to delete proposals",
					zap.Stringer("lid", oldest),
					zap.Error(err),
				)
			}
			proposalLatency.Observe(time.Since(t0).Seconds())
			t1 := time.Now()
			if err := certificates.DeleteCertBefore(db, oldest); err != nil {
				logger.Error("failed to delete certificates",
					zap.Stringer("lid", oldest),
					zap.Error(err),
				)
			}
			certLatency.Observe(time.Since(t1).Seconds())
			t2 := time.Now()
			if err := transactions.DeleteProposalTxsBefore(db, oldest); err != nil {
				logger.Error("failed to delete proposal tx mapping",
					zap.Stringer("lid", oldest),
					zap.Error(err),
				)
			}
			propTxLatency.Observe(time.Since(t2).Seconds())
		}
	}
}
