package sim

import (
	"math/rand"
	"path/filepath"

	"github.com/spacemeshos/go-spacemesh/activation"
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
	return rng.Intn(ints[1]-ints[0]) + ints[0]
}
