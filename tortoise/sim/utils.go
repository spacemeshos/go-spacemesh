package sim

import (
	"math/rand"
	"path/filepath"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

var goldenATX = types.ATXID{1, 1, 1}

const (
	meshpath = "mesh"
	atxpath  = "atx"
)

func newAtxDB(logger log.Log, mdb *mesh.DB, conf config) *activation.DB {
	var (
		db  database.Database
		err error
	)
	if len(conf.Path) == 0 {
		db = database.NewMemDatabase()
	} else {
		db, err = database.NewLDBDatabase(filepath.Join(conf.Path, atxpath), 0, 0, logger)
		if err != nil {
			panic(err)
		}
	}
	return activation.NewDB(db, nil, nil, mdb, conf.LayersPerEpoch, goldenATX, nil, logger)
}

func newMeshDB(logger log.Log, conf config) *mesh.DB {
	if len(conf.Path) > 0 {
		db, err := mesh.NewPersistentMeshDB(filepath.Join(conf.Path, meshpath), 20, logger)
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

var _ blocks.BeaconGetter = (*beaconStore)(nil)

func newBeaconStore() *beaconStore {
	return &beaconStore{beacons: map[types.EpochID][]byte{}}
}

// TODO(dshulyak) replaced it with real beacon store so that we can enable persistence
// for benchmarks.
type beaconStore struct {
	beacons map[types.EpochID][]byte
}

func (b *beaconStore) GetBeacon(eid types.EpochID) ([]byte, error) {
	beacon, exist := b.beacons[eid-1]
	if !exist {
		return nil, database.ErrNotFound
	}
	return beacon, nil
}

func (b *beaconStore) StoreBeacon(eid types.EpochID, beacon []byte) {
	b.beacons[eid] = beacon
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
