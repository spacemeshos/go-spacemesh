package atxsdata

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

func Warm(db *sql.Database, keep types.EpochID) (*Data, error) {
	cache := New()
	tx, err := db.Tx(context.Background())
	if err != nil {
		return nil, err
	}
	defer tx.Release()
	if err := Warmup(tx, cache, keep); err != nil {
		return nil, fmt.Errorf("warmup %w", err)
	}
	return cache, nil
}

func Warmup(db sql.Executor, cache *Data, keep types.EpochID) error {
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

	var ierr error
	if err := atxs.IterateAtxsData(db, cache.Evicted(), latest,
		func(
			id types.ATXID,
			node types.NodeID,
			epoch types.EpochID,
			coinbase types.Address,
			weight,
			base,
			height uint64,
			nonce *types.VRFPostIndex,
			malicious bool,
		) bool {
			if nonce == nil {
				ierr = errors.New("missing nonce")
				return false
			}
			cache.Add(
				epoch+1,
				node,
				coinbase,
				id,
				weight,
				base,
				height,
				*nonce,
				malicious,
			)
			return true
		}); err != nil {
		return err
	}
	return ierr
}
