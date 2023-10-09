package sim

import (
	"math/rand"
	"path/filepath"

	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
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
