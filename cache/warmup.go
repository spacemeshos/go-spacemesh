package cache

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

func Warm(db *sql.Database, opts ...Opt) (*Cache, error) {
	cache := New(opts...)
	if err := Warmup(db, cache); err != nil {
		return nil, fmt.Errorf("warmup %w", err)
	}
	return cache, nil
}

func Warmup(db *sql.Database, cache *Cache) error {
	latest, err := atxs.LatestEpoch(db)
	if err != nil {
		return err
	}
	applied, err := layers.GetLastApplied(db)
	if err != nil {
		return err
	}
	evicted := applied.GetEpoch() - cache.TargetCapacity()
	cache.Evict(evicted)
	var ierr error
	if err := atxs.IterateAtxs(db, evicted+1, latest, func(vatx *types.VerifiedActivationTx) bool {
		nonce, err := atxs.VRFNonce(db, vatx.SmesherID, vatx.TargetEpoch())
		if err != nil {
			ierr = err
			return false
		}
		malicious, err := identities.IsMalicious(db, vatx.SmesherID)
		if err != nil {
			ierr = err
			return false
		}
		cache.Add(vatx.TargetEpoch(), vatx.SmesherID, vatx.ID(), ToATXData(vatx.ToHeader(), nonce, malicious))
		return true
	}); err != nil {
		return err
	}
	return ierr
}

func ToATXData(atx *types.ActivationTxHeader, nonce types.VRFPostIndex, malicious bool) *ATXData {
	return &ATXData{
		Weight:     atx.GetWeight(),
		BaseHeight: atx.BaseTickHeight,
		Height:     atx.TickHeight(),
		Nonce:      nonce,
		Malicious:  malicious,
	}
}
