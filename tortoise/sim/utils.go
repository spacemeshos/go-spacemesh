package sim

import (
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

const (
	atxpath = "atx"
)

func newCacheDB(tb testing.TB, logger *zap.Logger, conf config) *datastore.CachedDB {
	var (
		db  sql.StateDatabase
		err error
	)
	if len(conf.Path) == 0 {
		db = statesql.InMemoryTest(tb)
	} else {
		db, err = statesql.Open(filepath.Join(conf.Path, atxpath), sql.WithMigrationsDisabled())
		if err != nil {
			panic(err)
		}
	}
	cdb := datastore.NewCachedDB(db, logger)
	tb.Cleanup(func() { assert.NoError(tb, cdb.Close()) })
	return cdb
}

func intInRange(rng *rand.Rand, ints [2]int) uint32 {
	if ints[0] == ints[1] {
		return uint32(ints[0])
	}
	return uint32(rng.Intn(ints[1]-ints[0]) + ints[0])
}
