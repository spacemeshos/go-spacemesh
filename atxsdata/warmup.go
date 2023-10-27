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
	if err := atxs.IterateAtxs(db, cache.Evicted(), latest, func(vatx *types.VerifiedActivationTx) bool {
		nonce, err := atxs.VRFNonce(db, vatx.SmesherID, vatx.TargetEpoch())
		if err != nil {
			ierr = fmt.Errorf("missing nonce %w", err)
			return false
		}
		malicious, err := identities.IsMalicious(db, vatx.SmesherID)
		if err != nil {
			ierr = err
			return false
		}
		cache.Add(
			vatx.TargetEpoch(),
			vatx.SmesherID,
			vatx.ID(),
			vatx.GetWeight(),
			vatx.BaseTickHeight(),
			vatx.TickHeight(),
			nonce,
			malicious,
		)
		return true
	}); err != nil {
		return err
	}
	return ierr
}
