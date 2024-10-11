package atxsdata

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

func Warm(db sql.StateDatabase, keep types.EpochID, logger *zap.Logger) (*Data, error) {
	cache := New()
	tx, err := db.Tx(context.Background())
	if err != nil {
		return nil, err
	}
	defer tx.Release()
	if err := Warmup(tx, cache, keep, logger); err != nil {
		return nil, fmt.Errorf("warmup %w", err)
	}
	return cache, nil
}

func Warmup(db sql.Executor, cache *Data, keep types.EpochID, logger *zap.Logger) error {
	latest, err := atxs.LatestEpoch(db)
	if err != nil {
		return err
	}
	applied, err := layers.GetLastApplied(db)
	if err != nil {
		return err
	}
	var evict types.EpochID
	if applied.GetEpoch() > keep {
		evict = applied.GetEpoch() - keep - 1
	}
	cache.EvictEpoch(evict)

	from := cache.Evicted()
	logger.Info("Reading ATXs from DB",
		zap.Uint32("from epoch", from.Uint32()),
		zap.Uint32("to epoch", latest.Uint32()),
	)
	start := time.Now()
	var processed int
	err = atxs.IterateAtxsData(db, cache.Evicted(), latest,
		func(
			id types.ATXID,
			node types.NodeID,
			epoch types.EpochID,
			coinbase types.Address,
			weight,
			base,
			height uint64,
			nonce types.VRFPostIndex,
		) bool {
			cache.Add(
				epoch+1,
				node,
				coinbase,
				id,
				weight,
				base,
				height,
				nonce,
				false,
			)
			processed += 1
			if processed%1_000_000 == 0 {
				logger.Debug("Processed 1M", zap.Int("total", processed))
			}
			return true
		})
	if err != nil {
		return fmt.Errorf("warming up atxdata with ATXs: %w", err)
	}
	logger.Info("Finished reading ATXs. Starting reading malfeasance", zap.Duration("duration", time.Since(start)))
	start = time.Now()
	err = identities.IterateOps(db, builder.Operations{}, func(id types.NodeID, _ []byte, _ time.Time) bool {
		cache.SetMalicious(id)
		return true
	})
	if err != nil {
		return fmt.Errorf("warming up atxdata with malfeasance: %w", err)
	}
	logger.Info("Finished reading malfeasance", zap.Duration("duration", time.Since(start)))
	return nil
}
