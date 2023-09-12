package mesh

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
			if err := proposals.Delete(db, oldest); err != nil {
				logger.Error("failed to delete proposals",
					zap.Stringer("lid", oldest),
					zap.Error(err),
				)
			}
			logger.Info("proposals pruned", zap.Stringer("lid", oldest))
			if err := certificates.DeleteCert(db, oldest); err != nil {
				logger.Error("failed to delete certificates",
					zap.Stringer("lid", oldest),
					zap.Error(err),
				)
			}
			logger.Info("certificates pruned", zap.Stringer("lid", oldest))
			if err := transactions.DeleteProposalTxs(db, oldest); err != nil {
				logger.Error("failed to delete proposal tx mapping",
					zap.Stringer("lid", oldest),
					zap.Error(err),
				)
			}
			logger.Info("proposal tx pruned", zap.Stringer("lid", oldest))
		}
	}
}
