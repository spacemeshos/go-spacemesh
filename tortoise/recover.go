package tortoise

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/system"
)

// Recover tortoise state from database.
func Recover(db *datastore.CachedDB, beacon system.BeaconGetter, opts ...Opt) (*Tortoise, error) {
	trtl, err := New(opts...)
	if err != nil {
		return nil, err
	}
	latest, err := ballots.LatestLayer(db)
	if err != nil {
		return nil, fmt.Errorf("failed to load latest known layer: %v", err)
	}

	if latest == 0 && types.GetEffectiveGenesis() != types.FirstEffectiveGenesis() {
		first := types.GetEffectiveGenesis().GetEpoch()
		for _, epoch := range []types.EpochID{first, first + 1} {
			if err := db.IterateEpochATXHeaders(epoch, func(header *types.ActivationTxHeader) bool {
				trtl.OnAtx(header)
				return true
			}); err != nil {
				return nil, err
			}
		}
	}
	if latest <= types.GetEffectiveGenesis() {
		return trtl, nil
	}
	for lid := types.GetEffectiveGenesis().Add(1); !lid.After(latest); lid = lid.Add(1) {
		if err := RecoverLayer(context.Background(), trtl, db, beacon, lid); err != nil {
			return nil, fmt.Errorf("failed to load tortoise state at layer %d: %w", lid, err)
		}
	}
	return trtl, nil
}

func RecoverLayer(ctx context.Context, trtl *Tortoise, db *datastore.CachedDB, beacon system.BeaconGetter, lid types.LayerID) error {
	if lid.FirstInEpoch() {
		if err := db.IterateEpochATXHeaders(lid.GetEpoch(), func(header *types.ActivationTxHeader) bool {
			trtl.OnAtx(header)
			return true
		}); err != nil {
			return err
		}
		beacon, err := beacon.GetBeacon(lid.GetEpoch())
		if err != nil && !errors.Is(err, sql.ErrNotFound) {
			return err
		}
		if err == nil {
			trtl.OnBeacon(lid.GetEpoch(), beacon)
		}
	}
	blocksrst, err := blocks.Layer(db, lid)
	if err != nil {
		return err
	}
	for _, block := range blocksrst {
		valid, err := blocks.IsValid(db, block.ID())
		if err != nil && errors.Is(err, sql.ErrNotFound) {
			return err
		}
		if valid {
			trtl.OnValidBlock(block.ToVote())
		} else {
			trtl.OnBlock(block.ToVote())
		}
		hare, err := certificates.GetHareOutput(db, lid)
		if err != nil && !errors.Is(err, sql.ErrNotFound) {
			return err
		}
		if err == nil {
			trtl.OnHareOutput(lid, hare)
		}
	}
	ballotsrst, err := ballots.Layer(db, lid)
	if err != nil {
		return err
	}
	for _, ballot := range ballotsrst {
		trtl.OnBallot(ballot)
	}
	coin, err := layers.GetWeakCoin(db, lid)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return err
	}
	if err == nil {
		trtl.OnWeakCoin(lid, coin)
	}
	trtl.TallyVotes(ctx, lid)
	return nil
}
