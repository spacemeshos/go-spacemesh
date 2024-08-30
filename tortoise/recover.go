package tortoise

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/atxcache"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/beacons"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

// Recover tortoise state from database.
func Recover(
	ctx context.Context,
	db sql.Executor,
	atxdata *atxcache.Cache,
	current types.LayerID,
	opts ...Opt,
) (*Tortoise, error) {
	trtl, err := New(atxdata, opts...)
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
	if size := trtl.cfg.WindowSizeLayers(applied); applied > size {
		// we want to emulate the same condition as during genesis with one difference.
		// genesis starts with zero opinion (aggregated hash) - see computeOpinion method.
		// but in this case first processed layer should use non-zero opinion of the the previous layer.

		window := applied - size
		// we start tallying votes from the first layer of the epoch to guarantee that we load reference ballots.
		// reference ballots track beacon and eligibilities
		window = window.GetEpoch().FirstLayer()
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

	for _, id := range atxdata.MaliciousIdentities() {
		trtl.OnMalfeasance(id)
	}

	if types.GetEffectiveGenesis() != types.FirstEffectiveGenesis() {
		// need to load the golden atxs after a checkpoint recovery
		if err := recoverEpoch(types.GetEffectiveGenesis().Add(1).GetEpoch(), trtl, db, atxdata); err != nil {
			return nil, err
		}
	}

	valid, err := blocks.LastValid(db)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, fmt.Errorf("get last valid: %w", err)
	}
	if err == nil {
		trtl.UpdateVerified(valid)
	}
	trtl.UpdateLastLayer(last)

	for lid := start; !lid.After(last); lid = lid.Add(1) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if err := RecoverLayer(ctx, trtl, db, atxdata, lid, trtl.OnRecoveredBallot); err != nil {
			return nil, fmt.Errorf("failed to load tortoise state at layer %d: %w", lid, err)
		}
	}

	// load activations from future epochs that are not yet referenced by the ballots
	atxsEpoch, err := atxs.LatestEpoch(db)
	if err != nil {
		return nil, fmt.Errorf("failed to load latest epoch: %w", err)
	}
	atxsEpoch++ // recoverEpoch expects target epoch
	if last.GetEpoch() != atxsEpoch {
		for eid := last.GetEpoch() + 1; eid <= atxsEpoch; eid++ {
			if err := recoverEpoch(eid, trtl, db, atxdata); err != nil {
				return nil, err
			}
		}
	}

	last = min(last, current)
	if last < start {
		return trtl, nil
	}
	trtl.TallyVotes(ctx, last)
	// find topmost layer that was already applied with same result
	// and reset pending so that result for that layer is not returned
	for prev := valid; prev >= start; prev-- {
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

func recoverEpoch(target types.EpochID, trtl *Tortoise, db sql.Executor, atxdata *atxcache.Cache) error {
	atxdata.IterateInEpoch(target, func(id types.ATXID, atx *atxcache.ATX) {
		trtl.OnAtx(target, id, atx)
	}, false)

	beacon, err := beacons.Get(db, target)
	if err == nil && beacon != types.EmptyBeacon {
		trtl.OnBeacon(target, beacon)
	}
	return nil
}

type ballotFunc func(*types.BallotTortoiseData)

func RecoverLayer(
	ctx context.Context,
	trtl *Tortoise,
	db sql.Executor,
	atxdata *atxcache.Cache,
	lid types.LayerID,
	onBallot ballotFunc,
) error {
	if lid.FirstInEpoch() {
		if err := recoverEpoch(lid.GetEpoch(), trtl, db, atxdata); err != nil {
			return err
		}
	}
	blocksrst, err := blocks.Layer(db, lid)
	if err != nil {
		return err
	}

	results := map[types.BlockHeader]bool{}
	for _, block := range blocksrst {
		valid, err := blocks.IsValid(db, block.ID())
		if err != nil && errors.Is(err, sql.ErrNotFound) {
			return err
		}
		results[block.ToVote()] = valid
	}
	var hareResult *types.BlockID
	if trtl.WithinHdist(lid) {
		hare, err := certificates.GetHareOutput(db, lid)
		if err != nil && !errors.Is(err, sql.ErrNotFound) {
			return err
		}
		if err == nil {
			hareResult = &hare
		}
	}
	trtl.OnRecoveredBlocks(lid, results, hareResult)
	// tortoise votes according to the hare only within hdist (protocol parameter).
	// also node is free to prune certificates outside hdist to minimize space usage.

	// NOTE(dshulyak) we loaded information about malicious identities earlier.
	ballotsrst, err := ballots.LayerNoMalicious(db, lid)
	if err != nil {
		return err
	}
	// NOTE(dshulyak) it is done in two steps so that if ballot from the same layer was used
	// as reference or base ballot we will be able to decode it.
	// it might be possible to invalidate such ballots, but until then this is required
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
