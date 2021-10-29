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
		st := &state{db: database.NewMemDatabase(), log: logtest.New(tb)}

		verifiedGen, ok := quick.Value(reflect.TypeOf(uint32(0)), rng)
		require.True(tb, ok)
		verified := verifiedGen.Interface().(uint32)
		// verified between 200 and 1000
		verified = max(200, min(verified, 1000))

		st.Last = types.NewLayerID(verified + 100)
		st.Verified = types.NewLayerID(verified)

		st.GoodBlocksIndex = map[types.BlockID]bool{}
		st.BlockOpinionsByLayer = map[types.LayerID]map[types.BlockID]Opinion{}
		st.BlockLayer = map[types.BlockID]types.LayerID{}

		for i := 0; i < 200; i++ {
			layerGen, ok := quick.Value(reflect.TypeOf(uint32(0)), rng)
			require.True(tb, ok)
			block1Gen, ok := quick.Value(reflect.TypeOf(types.BlockID{}), rng)
			require.True(tb, ok)
			block2Gen, ok := quick.Value(reflect.TypeOf(types.BlockID{}), rng)
			require.True(tb, ok)

			layerVal := layerGen.Interface().(uint32)
			layer := types.NewLayerID(layerVal % verified)
			if _, exist := st.BlockOpinionsByLayer[layer]; !exist {
				st.BlockOpinionsByLayer[layer] = map[types.BlockID]Opinion{}
			}
			block1 := block1Gen.Interface().(types.BlockID)
			if _, exist := st.BlockOpinionsByLayer[layer][block1]; !exist {
				st.BlockOpinionsByLayer[layer][block1] = Opinion{}
				st.BlockLayer[block1] = layer
			}
			st.GoodBlocksIndex[block1] = false
			block2 := block2Gen.Interface().(types.BlockID)
			vecGen, ok := quick.Value(reflect.TypeOf(vec{}), rng)
			require.True(tb, ok)
			val := vecGen.Interface().(vec)
			val.Flushed = false
			st.BlockOpinionsByLayer[layer][block1][block2] = val
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
		st.BlockOpinionsByLayer = nil
		st.GoodBlocksIndex = nil
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
		layers := make([]types.LayerID, 0, len(st.BlockOpinionsByLayer))
		for layer := range st.BlockOpinionsByLayer {
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
