package eligibility

import (
	"context"
	"errors"
	"fmt"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/system"
)

const activesCacheSize = 5 // we don't expect to handle more than two layers concurrently

type identityWeight struct {
	atx    types.ATXID
	weight uint64
}
type cachedActiveSet struct {
	set   map[types.NodeID]identityWeight
	total uint64
}

func (c *cachedActiveSet) atxs() []types.ATXID {
	atxs := make([]types.ATXID, 0, len(c.set))
	for _, id := range c.set {
		atxs = append(atxs, id.atx)
	}
	return atxs
}

type ActiveSetCache struct {
	mu       sync.Mutex
	cache    *lru.Cache[types.EpochID, *cachedActiveSet]
	fallback map[types.EpochID][]types.ATXID
	sync     system.SyncStateProvider
	// NOTE(dshulyak) on switch from synced to not synced reset the cache
	// to cope with https://github.com/spacemeshos/go-spacemesh/issues/4552
	// until graded oracle is implemented
	synced bool

	beacons  BeaconProvider
	db       sql.Executor
	atxsdata *atxsdata.Data
	log      *zap.Logger
}

func NewActiveSetCache(
	beacons BeaconProvider,
	db sql.Executor,
	atxsdata *atxsdata.Data,
	log *zap.Logger,
) (*ActiveSetCache, error) {
	cache, err := lru.New[types.EpochID, *cachedActiveSet](activesCacheSize)
	if err != nil {
		return nil, fmt.Errorf("create lru cache for active set: %w", err)
	}
	return &ActiveSetCache{
		cache:    cache,
		fallback: make(map[types.EpochID][]types.ATXID),
		beacons:  beacons,
		db:       db,
		atxsdata: atxsdata,
		log:      log,
	}, nil
}

func (o *ActiveSetCache) SetSync(sync system.SyncStateProvider) {
	o.sync = sync
}

func (o *ActiveSetCache) resetCacheOnSynced(ctx context.Context) {
	synced := o.synced
	o.synced = o.sync.IsSynced(ctx)
	if !synced && o.synced {
		o.cache.Purge()
	}
}

// Returns a set of all active node IDs in the specified epoch.
func (o *ActiveSetCache) actives(ctx context.Context, targetEpoch types.EpochID) (*cachedActiveSet, error) {
	if !targetEpoch.FirstLayer().After(types.GetEffectiveGenesis()) {
		return nil, errEmptyActiveSet
	}
	o.log.Debug("hare oracle getting active set",
		log.ZContext(ctx),
		zap.Uint32("target_epoch", targetEpoch.Uint32()),
	)

	o.mu.Lock()
	defer o.mu.Unlock()
	o.resetCacheOnSynced(ctx)
	if value, exists := o.cache.Get(targetEpoch); exists {
		return value, nil
	}
	activeSet, err := o.computeActiveSet(ctx, targetEpoch)
	if err != nil {
		return nil, err
	}
	if len(activeSet) == 0 {
		return nil, errEmptyActiveSet
	}
	activeWeights, err := o.computeActiveWeights(targetEpoch, activeSet)
	if err != nil {
		return nil, err
	}

	aset := &cachedActiveSet{set: activeWeights}
	for _, aweight := range activeWeights {
		aset.total += aweight.weight
	}
	o.log.Debug("got hare active set", log.ZContext(ctx), zap.Int("count", len(activeWeights)))
	o.cache.Add(targetEpoch, aset)
	return aset, nil
}

func (o *ActiveSetCache) ActiveSet(ctx context.Context, targetEpoch types.EpochID) ([]types.ATXID, error) {
	aset, err := o.actives(ctx, targetEpoch)
	if err != nil {
		return nil, err
	}
	return aset.atxs(), nil
}

func (o *ActiveSetCache) computeActiveSet(ctx context.Context, targetEpoch types.EpochID) ([]types.ATXID, error) {
	activeSet, ok := o.fallback[targetEpoch]
	if ok {
		o.log.Debug("using fallback active set",
			log.ZContext(ctx),
			zap.Uint32("target_epoch", targetEpoch.Uint32()),
			zap.Int("size", len(activeSet)),
		)
		return activeSet, nil
	}

	activeSet, err := miner.ActiveSetFromEpochFirstBlock(o.db, targetEpoch)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, err
	}
	if len(activeSet) == 0 {
		return o.activeSetFromRefBallots(ctx, targetEpoch)
	}
	return activeSet, nil
}

func (o *ActiveSetCache) computeActiveWeights(
	targetEpoch types.EpochID,
	activeSet []types.ATXID,
) (map[types.NodeID]identityWeight, error) {
	identities := make(map[types.NodeID]identityWeight, len(activeSet))
	for _, id := range activeSet {
		atx := o.atxsdata.Get(targetEpoch, id)
		if atx == nil {
			return nil, fmt.Errorf("oracle: missing atx in atxsdata %s/%s", targetEpoch, id.ShortString())
		}
		identities[atx.Node] = identityWeight{atx: id, weight: atx.Weight}
	}
	return identities, nil
}

func (o *ActiveSetCache) activeSetFromRefBallots(ctx context.Context, epoch types.EpochID) ([]types.ATXID, error) {
	beacon, err := o.beacons.Beacon(ctx, epoch)
	if err != nil {
		return nil, fmt.Errorf("get beacon: %w", err)
	}
	ballotsrst, err := ballots.AllFirstInEpoch(o.db, epoch)
	if err != nil {
		return nil, fmt.Errorf("first in epoch %d: %w", epoch, err)
	}
	activeMap := make(map[types.ATXID]struct{}, len(ballotsrst))
	for _, ballot := range ballotsrst {
		if ballot.EpochData == nil {
			o.log.Error("invalid data. first ballot doesn't have epoch data", zap.Inline(ballot))
			continue
		}
		if ballot.EpochData.Beacon != beacon {
			o.log.Debug("beacon mismatch", zap.Stringer("local", beacon), zap.Object("ballot", ballot))
			continue
		}
		actives, err := activesets.Get(o.db, ballot.EpochData.ActiveSetHash)
		if err != nil {
			o.log.Error("failed to get active set",
				zap.String("actives hash", ballot.EpochData.ActiveSetHash.ShortString()),
				zap.String("ballot ", ballot.ID().String()),
				zap.Error(err),
			)
			continue
		}
		for _, id := range actives.Set {
			activeMap[id] = struct{}{}
		}
	}
	o.log.Warn("using tortoise active set",
		zap.Int("actives size", len(activeMap)),
		zap.Uint32("epoch", epoch.Uint32()),
		zap.Stringer("beacon", beacon),
	)
	return maps.Keys(activeMap), nil
}

func (o *ActiveSetCache) UpdateActiveSet(epoch types.EpochID, activeSet []types.ATXID) {
	o.log.Debug("received activeset update",
		zap.Uint32("epoch", epoch.Uint32()),
		zap.Int("size", len(activeSet)),
	)
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.fallback[epoch]; ok {
		o.log.Debug("fallback active set already exists", zap.Uint32("epoch", epoch.Uint32()))
		return
	}
	o.fallback[epoch] = activeSet
}

func (o *ActiveSetCache) TotalWeight(ctx context.Context, epoch types.EpochID) (uint64, error) {
	actives, err := o.actives(ctx, epoch)
	if err != nil {
		return 0, err
	}
	return actives.total, nil
}

func (o *ActiveSetCache) MinerWeight(ctx context.Context, epoch types.EpochID, id types.NodeID) (uint64, error) {
	actives, err := o.actives(ctx, epoch)
	if err != nil {
		return 0, err
	}

	w, ok := actives.set[id]
	if !ok {
		return 0, fmt.Errorf("%w: %v", ErrNotActive, id)
	}
	return w.weight, nil
}
