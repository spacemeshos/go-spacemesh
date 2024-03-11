package atxsdata

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

func Warm(db *sql.Database, opts ...Opt) (*Data, error) {
	cache := New(opts...)
	tx, err := db.Tx(context.Background())
	if err != nil {
		return nil, err
	}
	defer tx.Release()
	if err := Warmup(tx, cache); err != nil {
		return nil, fmt.Errorf("warmup %w", err)
	}
	return cache, nil
}

func Warmup(db sql.Executor, cache *Data) error {
	latest, err := atxs.LatestEpoch(db)
	if err != nil {
		return err
	}
	applied, err := layers.GetLastApplied(db)
	if err != nil {
		return err
	}
	cache.OnEpoch(applied.GetEpoch())

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
		) bool {
			target := epoch + 1
			nonce, err := atxs.VRFNonce(db, node, target)
			if err != nil {
				ierr = fmt.Errorf("missing nonce %w", err)
				return false
			}
			malicious, err := identities.IsMalicious(db, node)
			if err != nil {
				ierr = err
				return false
			}
			cache.Add(
				target,
				node,
				coinbase,
				id,
				weight,
				base,
				height,
				nonce,
				malicious,
			)
			return true
		}); err != nil {
		return err
	}
	return ierr
}
