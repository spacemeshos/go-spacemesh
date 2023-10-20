package tortoise

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/beacons"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

// Recover tortoise state from database.
func Recover(
	ctx context.Context,
	db *datastore.CachedDB,
	current types.LayerID,
	opts ...Opt,
) (*Tortoise, error) {
	trtl, err := New(opts...)
	if err != nil {
		return nil, err
	}

	last, err := ballots.LatestLayer(db)
	if err != nil {
		return nil, fmt.Errorf("failed to load latest known layer: %w", err)
	}

	applied, err := layers.GetLastApplied(db)
	if err != nil {
		return nil, fmt.Errorf("get last applied: %w", err)
	}
	start := types.GetEffectiveGenesis() + 1
	if applied > types.LayerID(trtl.cfg.WindowSize) {
		window := applied - types.LayerID(trtl.cfg.WindowSize)
		window = window.GetEpoch().
			FirstLayer()
			// windback to the start of the epoch to load ref ballots
		if window > start {
			prev, err1 := layers.GetAggregatedHash(db, window-1)
			opinion, err2 := layers.GetAggregatedHash(db, window)
			if err1 == nil && err2 == nil {
				// tortoise will need reference to previous layer
				trtl.RecoverFrom(window, opinion, prev)
				start = window
			}
		}
	}

	malicious, err := identities.GetMalicious(db)
	if err != nil {
		return nil, fmt.Errorf("recover malicious %w", err)
	}
	for _, id := range malicious {
		trtl.OnMalfeasance(id)
	}

	if types.GetEffectiveGenesis() != types.FirstEffectiveGenesis() {
		// need to load the golden atxs after a checkpoint recovery
		if err := recoverEpoch(types.GetEffectiveGenesis().Add(1).GetEpoch(), trtl, db); err != nil {
			return nil, err
		}
	}

	epoch, err := atxs.LatestEpoch(db)
	if err != nil {
		return nil, fmt.Errorf("failed to load latest epoch: %w", err)
	}
	epoch++ // recoverEpoch expects target epoch, rather than publish
	if last.GetEpoch() != epoch {
		for eid := last.GetEpoch(); eid <= epoch; eid++ {
			if err := recoverEpoch(eid, trtl, db); err != nil {
				return nil, err
			}
		}
	}
	for lid := start; !lid.After(last); lid = lid.Add(1) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if err := RecoverLayer(ctx, trtl, db, lid, trtl.OnRecoveredBallot); err != nil {
			return nil, fmt.Errorf("failed to load tortoise state at layer %d: %w", lid, err)
		}
	}
	if last == 0 {
		last = current
	} else {
		last = min(last, current)
	}
	if last < start {
		return trtl, nil
	}
	trtl.TallyVotes(ctx, last)
	// find topmost layer that was already applied and reset pending
	// so that result for that layer is not returned
	for prev := last - 1; prev >= start; prev-- {
		opinion, err := layers.GetAggregatedHash(db, prev)
		if err == nil && opinion != types.EmptyLayerHash {
			if trtl.OnApplied(prev, opinion) {
				break
			}
		}
		if err != nil && !errors.Is(err, sql.ErrNotFound) {
			return nil, fmt.Errorf("check opinion %w", err)
		}
	}
	return trtl, nil
}

func recoverEpoch(epoch types.EpochID, trtl *Tortoise, db *datastore.CachedDB) error {
	if err := db.IterateEpochATXHeaders(epoch, func(header *types.ActivationTxHeader) error {
		trtl.OnAtx(header.ToData())
		return nil
	}); err != nil {
		return err
	}
	beacon, err := beacons.Get(db, epoch)
	if err == nil && beacon != types.EmptyBeacon {
		trtl.OnBeacon(epoch, beacon)
	}
	return nil
}

type ballotFunc func(*types.BallotTortoiseData)

func RecoverLayer(
	ctx context.Context,
	trtl *Tortoise,
	db *datastore.CachedDB,
	lid types.LayerID,
	onBallot ballotFunc,
) error {
	if lid.FirstInEpoch() {
		if err := recoverEpoch(lid.GetEpoch(), trtl, db); err != nil {
			return err
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
	// NOTE(dshulyak) we loaded information about malicious identities earlier.
	ballotsrst, err := ballots.LayerNoMalicious(db, lid)
	if err != nil {
		return err
	}
	for _, ballot := range ballotsrst {
		if ballot.EpochData != nil {
			onBallot(ballot.ToTortoiseData())
		}
	}
	for _, ballot := range ballotsrst {
		if ballot.EpochData == nil {
			onBallot(ballot.ToTortoiseData())
		}
	}
	coin, err := layers.GetWeakCoin(db, lid)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return err
	} else if err == nil {
		trtl.OnWeakCoin(lid, coin)
	}
	return nil
}
