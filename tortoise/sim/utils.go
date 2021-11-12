package sim

import (
	"math/rand"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

var goldenATX = types.ATXID{1, 1, 1}

func newAtxDB(logger log.Log, mdb *mesh.DB, conf config) *activation.DB {
	return activation.NewDB(database.NewMemDatabase(), nil, nil, mdb, conf.LayersPerEpoch, goldenATX, nil, logger)
}

func intInRange(rng *rand.Rand, ints [2]int) int {
	return rng.Intn(ints[1]-ints[0]) + ints[0]
}
