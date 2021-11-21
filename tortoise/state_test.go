package tortoise

import (
	"context"
	"math/rand"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"testing/quick"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func min(x, y uint32) uint32 {
	if x < y {
		return x
	}
	return y
}

func max(x, y uint32) uint32 {
	if x > y {
		return x
	}
	return y
}

func makeStateGen(tb testing.TB, db database.Database, logger log.Log) func(rng *rand.Rand) *state {
	return func(rng *rand.Rand) *state {
		st := &state{db: db, log: logger}

		verifiedGen, ok := quick.Value(reflect.TypeOf(uint32(0)), rng)
		require.True(tb, ok)
		verified := verifiedGen.Interface().(uint32)
		// verified between 200 and 1000
		verified = max(200, min(verified, 1000))

		st.Last = types.NewLayerID(verified + 100)
		st.Verified = types.NewLayerID(verified)

		st.GoodBallotsIndex = map[types.BallotID]bool{}
		st.BallotOpinionsByLayer = map[types.LayerID]map[types.BallotID]Opinion{}
		st.BallotLayer = map[types.BallotID]types.LayerID{}
		st.BlockLayer = map[types.BlockID]types.LayerID{}
		st.refBallotBeacons = make(map[types.EpochID]map[types.BallotID][]byte)

		for i := 0; i < 200; i++ {
			layerGen, ok := quick.Value(reflect.TypeOf(uint32(0)), rng)
			require.True(tb, ok)
			ballotGen, ok := quick.Value(reflect.TypeOf(types.BallotID{}), rng)
			require.True(tb, ok)
			blockGen, ok := quick.Value(reflect.TypeOf(types.BlockID{}), rng)
			require.True(tb, ok)

			layerVal := layerGen.Interface().(uint32)
			layer := types.NewLayerID(layerVal % verified)
			if _, exist := st.BallotOpinionsByLayer[layer]; !exist {
				st.BallotOpinionsByLayer[layer] = map[types.BallotID]Opinion{}
			}
			ballot := ballotGen.Interface().(types.BallotID)
			if _, exist := st.BallotOpinionsByLayer[layer][ballot]; !exist {
				st.BallotOpinionsByLayer[layer][ballot] = Opinion{}
				st.BallotLayer[ballot] = layer
			}
			st.BlockLayer[types.BlockID(ballot)] = layer

			epoch := layer.GetEpoch()
			if _, ok := st.refBallotBeacons[epoch]; !ok {
				st.refBallotBeacons[epoch] = make(map[types.BallotID][]byte)
			}
			st.refBallotBeacons[epoch][ballot] = randomBytes(tb, 32)

			st.GoodBallotsIndex[ballot] = false
			block := blockGen.Interface().(types.BlockID)
			vecGen, ok := quick.Value(reflect.TypeOf(vec{}), rng)
			require.True(tb, ok)
			val := vecGen.Interface().(vec)
			val.Flushed = false
			st.BallotOpinionsByLayer[layer][ballot][block] = val
		}
		return st
	}
}

func TestStateRecover(t *testing.T) {
	require.NoError(t, quick.Check(func(st *state) bool {
		st.diffMode = true
		original := *st
		if !assert.NoError(t, st.Persist()) {
			return false
		}
		st.BallotOpinionsByLayer = nil
		st.GoodBallotsIndex = nil
		for i := 0; i < 2; i++ {
			if err := st.Recover(); err != nil {
				return false
			}
			if !assert.Equal(t, &original, st) {
				return false
			}
		}
		return true
	}, &quick.Config{
		Values: func(values []reflect.Value, rng *rand.Rand) {
			require.Len(t, values, 1)
			gen := makeStateGen(t, database.NewMemDatabase(), logtest.New(t))
			values[0] = reflect.ValueOf(gen(rng))
		},
	}))
}

func TestStateEvict(t *testing.T) {
	require.NoError(t, quick.Check(func(st *state) bool {
		layers := make([]types.LayerID, 0, len(st.BallotOpinionsByLayer))
		for layer := range st.BallotOpinionsByLayer {
			layers = append(layers, layer)
		}
		sort.Slice(layers, func(i, j int) bool {
			return layers[i].Before(layers[j])
		})
		// persist everything, otherwise evict will only delete data from memory
		if !assert.NoError(t, st.Persist()) {
			return false
		}

		if !assert.NoError(t, st.Evict(context.TODO(), layers[len(layers)/2])) {
			return false
		}

		// persists layers including LastEvicted, as it is recovered and compared in the test
		if !assert.NoError(t, st.Persist()) {
			return false
		}

		original := *st
		if !assert.NoError(t, st.Recover()) {
			return false
		}
		return assert.Equal(t, &original, st)
	}, &quick.Config{
		Values: func(values []reflect.Value, rng *rand.Rand) {
			require.Len(t, values, 1)
			gen := makeStateGen(t, database.NewMemDatabase(), logtest.New(t))
			values[0] = reflect.ValueOf(gen(rng))
		},
	}))
}

func TestStateRecoverNotFound(t *testing.T) {
	st := makeStateGen(t, database.NewMemDatabase(), logtest.New(t))(rand.New(rand.NewSource(1001)))
	require.ErrorIs(t, st.Recover(), database.ErrNotFound)
}

func TestEvictBeaconByEpoch(t *testing.T) {
	st := makeStateGen(t, database.NewMemDatabase(), logtest.New(t))(rand.New(rand.NewSource(1001)))
	st.refBallotBeacons = make(map[types.EpochID]map[types.BallotID][]byte)

	for b, l := range st.BallotLayer {
		epoch := l.GetEpoch()
		if _, ok := st.refBallotBeacons[epoch]; !ok {
			st.refBallotBeacons[epoch] = make(map[types.BallotID][]byte)
		}
		st.refBallotBeacons[epoch][b] = randomBytes(t, 32)
	}
	layers := make([]types.LayerID, 0, len(st.BallotOpinionsByLayer))
	for layer := range st.BallotOpinionsByLayer {
		layers = append(layers, layer)
	}
	sort.Slice(layers, func(i, j int) bool {
		return layers[i].Before(layers[j])
	})

	epochSize := len(st.refBallotBeacons)
	windowStart := layers[len(layers)/2]
	earliestEpoch := windowStart.GetEpoch()
	assert.NotEmpty(t, st.refBallotBeacons[earliestEpoch])
	require.NoError(t, st.Evict(context.TODO(), windowStart))

	// beacon cache should be evicted
	assert.LessOrEqual(t, len(st.refBallotBeacons), epochSize)
	// windowStart's epoch should still be there
	assert.NotEmpty(t, st.refBallotBeacons[earliestEpoch])
	// every epoch should be >= earliestEpoch
	for epoch := range st.refBallotBeacons {
		assert.GreaterOrEqual(t, epoch, earliestEpoch)
	}
}

func BenchmarkStatePersist(b *testing.B) {
	db, err := database.NewLDBDatabase(filepath.Join(b.TempDir(), "turtle_state"), 0, 0, logtest.New(b))
	require.NoError(b, err)

	st := makeStateGen(b, db, logtest.New(b))(rand.New(rand.NewSource(1001)))

	b.Run("New", func(b *testing.B) {
		st.diffMode = false
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := st.Persist(); err != nil {
				require.NoError(b, err)
			}
		}
	})
	b.Run("Repeat", func(b *testing.B) {
		st.diffMode = true
		require.NoError(b, st.Persist())
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := st.Persist(); err != nil {
				require.NoError(b, err)
			}
		}
	})
	b.Run("Old", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf, err := codec.Encode(st)
			if err != nil {
				require.NoError(b, err)
			}
			if err := db.Put([]byte("turtle"), buf); err != nil {
				require.NoError(b, err)
			}
		}
	})
}
