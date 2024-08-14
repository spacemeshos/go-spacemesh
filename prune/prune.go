package prune

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

type Opt func(*Pruner)

func WithLogger(logger *zap.Logger) Opt {
	return func(p *Pruner) {
		p.logger = logger
	}
}

func New(db *sql.Database, safeDist uint32, activesetEpoch types.EpochID, opts ...Opt) *Pruner {
	p := &Pruner{
		logger:         zap.NewNop(),
		db:             db,
		safeDist:       safeDist,
		activesetEpoch: activesetEpoch,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

type Pruner struct {
	logger         *zap.Logger
	db             *sql.Database
	safeDist       uint32
	activesetEpoch types.EpochID
}

func Run(ctx context.Context, p *Pruner, clock *timesync.NodeClock, interval time.Duration) {
	p.logger.With().Info("db pruning launched",
		zap.Uint32("dist", p.safeDist),
		zap.Uint32("active set epoch", p.activesetEpoch.Uint32()),
		zap.Duration("interval", interval),
	)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			current := clock.CurrentLayer()
			if err := p.Prune(current); err != nil {
				p.logger.Error("failed to prune",
					zap.Uint32("lid", current.Uint32()),
					zap.Uint32("dist", p.safeDist),
					zap.Error(err),
				)
			}
		}
	}
}

func (p *Pruner) Prune(current types.LayerID) error {
	oldest := current - types.LayerID(p.safeDist)
	start := time.Now()

	proposalLatency.Observe(time.Since(start).Seconds())
	start = time.Now()
	if err := certificates.DeleteCertBefore(p.db, oldest); err != nil {
		return err
	}
	certLatency.Observe(time.Since(start).Seconds())
	start = time.Now()
	if err := transactions.DeleteProposalTxsBefore(p.db, oldest); err != nil {
		return err
	}
	propTxLatency.Observe(time.Since(start).Seconds())
	if current.GetEpoch() > p.activesetEpoch {
		start = time.Now()
		epoch := current.GetEpoch()
		if epoch > 0 {
			epoch--
		}
		// current - 1 as activesets will be fetched in hare eligibility oracle
		// for example if we are in epoch 9, we want to prune 7 and below
		// as activesets from 8 will be still be needed at the beginning of epoch 8
		if err := activesets.DeleteBeforeEpoch(p.db, epoch); err != nil {
			return err
		}
		activeSetLatency.Observe(time.Since(start).Seconds())
	}
	return nil
}
