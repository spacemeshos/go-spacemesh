package miner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/activeset"
)

type activesetGenOpt func(*activeSetGenerator)

func withWallClock(clock clockwork.Clock) activesetGenOpt {
	return func(a *activeSetGenerator) {
		a.wallclock = clock
	}
}

func newActiveSetGenerator(
	cfg config,
	log *zap.Logger,
	db, localdb sql.Executor,
	atxsdata *atxsdata.Data,
	clock layerClock,
	opts ...activesetGenOpt,
) *activeSetGenerator {
	a := &activeSetGenerator{
		cfg:       cfg,
		log:       log,
		db:        db,
		localdb:   localdb,
		atxsdata:  atxsdata,
		clock:     clock,
		wallclock: clockwork.NewRealClock(),
	}
	for _, opt := range opts {
		opt(a)
	}
	a.fallback.data = map[types.EpochID][]types.ATXID{}
	return a
}

type activeSetGenerator struct {
	cfg config
	log *zap.Logger

	db, localdb sql.Executor
	atxsdata    *atxsdata.Data
	clock       layerClock
	wallclock   clockwork.Clock

	fallback struct {
		sync.Mutex
		data map[types.EpochID][]types.ATXID
	}

	// we use it to avoid running `prepare` method in parallel
	linearized sync.Mutex
}

func (p *activeSetGenerator) updateFallback(target types.EpochID, set []types.ATXID) {
	p.log.Info("received trusted activeset update",
		zap.Uint32("epoch_id", target.Uint32()),
		zap.Int("size", len(set)),
	)
	p.fallback.Lock()
	defer p.fallback.Unlock()
	if _, exists := p.fallback.data[target]; exists {
		p.log.Debug("fallback active set already exists", zap.Uint32("epoch_id", target.Uint32()))
		return
	}
	p.fallback.data[target] = set
}

// ensure tries to generate active set for the target epoch in configured number of tries,
// each try will be repeated after configured retry interval.
func (a *activeSetGenerator) ensure(ctx context.Context, target types.EpochID) {
	var err error
	for try := 0; try < a.cfg.activeSet.Tries; try++ {
		current := a.clock.CurrentLayer()
		// we run it here for side effects
		_, _, _, err = a.generate(current, target)
		if err == nil {
			return
		}
		a.log.Debug("failed to prepare active set", zap.Error(err), zap.Uint32("attempt", uint32(try)))
		select {
		case <-ctx.Done():
			return
		case <-a.wallclock.After(a.cfg.activeSet.RetryInterval):
		}
	}
	a.log.Warn("failed to prepare active set", zap.Error(err))
}

// generate generates activeset.
//
// It persists it in the local database, so that when node is restarted
// it doesn't have to redo the work.
//
// The method is expected to be called at any point in current epoch, and at the very end of the previous epoch.
func (p *activeSetGenerator) generate(
	current types.LayerID,
	target types.EpochID,
) (types.Hash32, uint64, []types.ATXID, error) {
	p.linearized.Lock()
	defer p.linearized.Unlock()

	id, setWeight, set, err := activeset.Get(p.localdb, activeset.Tortoise, target)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return id, 0, nil, fmt.Errorf("failed to get prepared active set: %w", err)
	}
	if err == nil {
		return id, setWeight, set, nil
	}

	p.fallback.Lock()
	fallback, exists := p.fallback.data[target]
	p.fallback.Unlock()

	start := time.Now()
	if exists {
		p.log.Info("generating activeset from trusted fallback",
			zap.Uint32("epoch_id", target.Uint32()),
			zap.Int("size", len(fallback)),
		)
		var err error
		setWeight, err = getSetWeight(p.atxsdata, target, fallback)
		if err != nil {
			return id, 0, nil, err
		}
		set = fallback
	} else {
		epochStart := p.clock.LayerToTime(target.FirstLayer())
		networkDelay := p.cfg.networkDelay
		p.log.Info("generating activeset from grades", zap.Uint32("epoch_id", target.Uint32()),
			zap.Time("epoch start", epochStart),
			zap.Duration("network delay", networkDelay),
		)
		result, err := activeSetFromGrades(p.db, target, epochStart, networkDelay)
		if err != nil {
			return id, 0, nil, err
		}
		if result.Total == 0 {
			return id, 0, nil, errors.New("empty active set")
		}
		if len(result.Set)*100/result.Total > p.cfg.goodAtxPercent {
			set = result.Set
			setWeight = result.Weight
		} else {
			p.log.Info("node was not synced during previous epoch. can't use activeset from grades",
				zap.Uint32("epoch_id", target.Uint32()),
				zap.Time("epoch start", epochStart),
				zap.Duration("network delay", networkDelay),
				zap.Int("total", result.Total),
				zap.Int("set", len(result.Set)),
				zap.Int("omitted", result.Total-len(result.Set)),
			)
		}
	}
	if set == nil && current > target.FirstLayer() {
		p.log.Info("generating activeset from first block",
			zap.Uint32("current", current.Uint32()),
			zap.Uint32("first", target.FirstLayer().Uint32()),
		)
		// see how it can be further improved https://github.com/spacemeshos/go-spacemesh/issues/5560
		var err error
		set, err = ActiveSetFromEpochFirstBlock(p.db, target)
		if err != nil {
			return id, 0, nil, err
		}
		setWeight, err = getSetWeight(p.atxsdata, target, set)
		if err != nil {
			return id, 0, nil, err
		}
	}
	if set != nil && setWeight == 0 {
		return id, 0, nil, errors.New("empty active set")
	}
	if set != nil {
		p.log.Info("prepared activeset",
			zap.Uint32("epoch_id", target.Uint32()),
			zap.Int("size", len(set)),
			zap.Uint64("weight", setWeight),
			zap.Duration("elapsed", time.Since(start)),
		)
		sort.Slice(set, func(i, j int) bool {
			return bytes.Compare(set[i].Bytes(), set[j].Bytes()) < 0
		})
		id := types.ATXIDList(set).Hash()
		if err := activeset.Add(p.localdb, activeset.Tortoise, target, id, setWeight, set); err != nil {
			return id, 0, nil, fmt.Errorf("failed to persist prepared active set for epoch %v: %w", target, err)
		}
		return id, setWeight, set, nil
	}
	return id, 0, nil, fmt.Errorf("failed to generate activeset for epoch %v", target)
}

type gradedActiveSet struct {
	// Set includes activations with highest grade.
	Set []types.ATXID
	// Weight of the activations in the Set.
	Weight uint64
	// Total number of activations in the database that targets requests epoch.
	Total int
}

// activeSetFromGrades includes activations with the highest grade.
// Such activations were received at least 4 network delays before the epoch start, and no malfeasance proof for
// identity was received before the epoch start.
//
// On mainnet we use 30minutes as a network delay parameter.
func activeSetFromGrades(
	db sql.Executor,
	target types.EpochID,
	epochStart time.Time,
	networkDelay time.Duration,
) (gradedActiveSet, error) {
	var (
		setWeight uint64
		set       []types.ATXID
		total     int
	)
	if err := atxs.IterateForGrading(db, target-1, func(id types.ATXID, atxtime, prooftime int64, weight uint64) bool {
		total++
		if gradeAtx(epochStart, networkDelay, atxtime, prooftime) == good {
			set = append(set, id)
			setWeight += weight
		}
		return true
	}); err != nil {
		return gradedActiveSet{}, fmt.Errorf("failed to iterate atxs that target epoch %v: %w", target, err)
	}
	return gradedActiveSet{
		Set:    set,
		Weight: setWeight,
		Total:  total,
	}, nil
}

func getSetWeight(atxsdata *atxsdata.Data, target types.EpochID, set []types.ATXID) (uint64, error) {
	var setWeight uint64
	for _, id := range set {
		atx := atxsdata.Get(target, id)
		if atx == nil {
			return 0, fmt.Errorf("atx %s/%s is missing in atxsdata", target, id.ShortString())
		}
		setWeight += atx.Weight
	}
	return setWeight, nil
}

// atxGrade describes the grade of an ATX as described in
// https://community.spacemesh.io/t/grading-atxs-for-the-active-set/335
//
// let s be the start of the epoch, and δ the network propagation time.
// grade 0: ATX was received at time t >= s-3δ, or an equivocation proof was received by time s-δ.
// grade 1: ATX was received at time t < s-3δ before the start of the epoch, and no equivocation proof by time s-δ.
// grade 2: ATX was received at time t < s-4δ, and no equivocation proof was received for that id until time s.
type atxGrade int

const (
	evil atxGrade = iota
	acceptable
	good
)

func gradeAtx(epochStart time.Time, networkDelay time.Duration, atxNsec, proofNsec int64) atxGrade {
	atx := time.Unix(0, atxNsec)
	proof := time.Unix(0, proofNsec)
	if atx.Before(epochStart.Add(-4*networkDelay)) && (proofNsec == 0 || !proof.Before(epochStart)) {
		return good
	} else if atx.Before(epochStart.Add(-3*networkDelay)) &&
		(proofNsec == 0 || !proof.Before(epochStart.Add(-networkDelay))) {
		return acceptable
	}
	return evil
}

func ActiveSetFromEpochFirstBlock(db sql.Executor, epoch types.EpochID) ([]types.ATXID, error) {
	bid, err := layers.FirstAppliedInEpoch(db, epoch)
	if err != nil {
		return nil, fmt.Errorf("first block in epoch %d not found: %w", epoch, err)
	}
	return activeSetFromBlock(db, bid)
}

func activeSetFromBlock(db sql.Executor, bid types.BlockID) ([]types.ATXID, error) {
	block, err := blocks.Get(db, bid)
	if err != nil {
		return nil, fmt.Errorf("actives get block: %w", err)
	}
	activeMap := make(map[types.ATXID]struct{})
	// the active set is the union of all active sets recorded in rewarded miners' ref ballot
	for _, r := range block.Rewards {
		activeMap[r.AtxID] = struct{}{}
		ballot, err := ballots.FirstInEpoch(db, r.AtxID, block.LayerIndex.GetEpoch())
		if err != nil {
			return nil, fmt.Errorf(
				"ballot for atx %v in epoch %v: %w",
				r.AtxID.ShortString(),
				block.LayerIndex.GetEpoch(),
				err,
			)
		}
		actives, err := activesets.Get(db, ballot.EpochData.ActiveSetHash)
		if err != nil {
			return nil, fmt.Errorf(
				"actives get active hash for ballot %s: %w",
				ballot.ID().String(),
				err,
			)
		}
		for _, id := range actives.Set {
			activeMap[id] = struct{}{}
		}
	}
	return maps.Keys(activeMap), nil
}
