package sim

import (
	"math/rand"
	"path/filepath"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/system"
)

const (
	atxpath = "atx"
)

func newCacheDB(logger log.Log, conf config) *datastore.CachedDB {
	var (
		db  *sql.Database
		err error
	)
	if len(conf.Path) == 0 {
		db = sql.InMemory()
	} else {
		db, err = sql.Open(filepath.Join(conf.Path, atxpath))
		if err != nil {
			panic(err)
		}
	}
	return datastore.NewCachedDB(db, logger)
}

func intInRange(rng *rand.Rand, ints [2]int) uint32 {
	if ints[0] == ints[1] {
		return uint32(ints[0])
	}
	return uint32(rng.Intn(ints[1]-ints[0]) + ints[0])
}

var _ system.BeaconGetter = (*beaconStore)(nil)

func newBeaconStore() *beaconStore {
	return &beaconStore{beacons: map[types.EpochID]types.Beacon{}}
}

// TODO(dshulyak) replaced it with real beacon store so that we can enable persistence
// for benchmarks.
type beaconStore struct {
	beacons map[types.EpochID]types.Beacon
}

func (b *beaconStore) GetBeacon(eid types.EpochID) (types.Beacon, error) {
	beacon, exist := b.beacons[eid-1]
	if !exist {
		return types.EmptyBeacon, sql.ErrNotFound
	}
	return beacon, nil
}

func (b *beaconStore) StoreBeacon(eid types.EpochID, beacon types.Beacon) {
	b.beacons[eid] = beacon
}

func (b *beaconStore) Delete(eid types.EpochID) {
	delete(b.beacons, eid)
}

func (b *beaconStore) Copy(other *beaconStore) {
	for eid, beacon := range other.beacons {
		_, exist := b.beacons[eid]
		if exist {
			continue
		}
		b.beacons[eid] = beacon
	}
}
