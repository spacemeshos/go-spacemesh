package sim

import (
	"math/rand"
	"os"
	"path/filepath"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/system"
)

var goldenATX = types.ATXID{1, 1, 1}

const (
	meshpath = "mesh"
	atxpath  = "atx"
)

func newAtxDB(logger log.Log, conf config) *activation.DB {
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
	return activation.NewDB(db, nil, conf.LayersPerEpoch, goldenATX, nil, logger)
}

func newMeshDB(logger log.Log, conf config) *mesh.DB {
	if len(conf.Path) > 0 {
		os.MkdirAll(filepath.Join(conf.Path, meshpath), os.ModePerm)
		db, err := mesh.NewPersistentMeshDB(sql.InMemory(), logger)
		if err != nil {
			panic(err)
		}
		return db
	}
	return mesh.NewMemMeshDB(logger)
}

func intInRange(rng *rand.Rand, ints [2]int) int {
	if ints[0] == ints[1] {
		return ints[0]
	}
	return rng.Intn(ints[1]-ints[0]) + ints[0]
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
		return types.EmptyBeacon, database.ErrNotFound
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
