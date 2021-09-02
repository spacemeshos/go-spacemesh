package tortoise

import (
	"math/rand"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"testing/quick"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeStateGen(tb testing.TB, db database.Database, logger log.Log) func(rng *rand.Rand) *state {
	return func(rng *rand.Rand) *state {
		st := &state{db: database.NewMemDatabase(), log: logtest.New(tb)}
		var layers [3]types.LayerID
		for i := range layers {
			layer, ok := quick.Value(reflect.TypeOf(types.LayerID{}), rng)
			require.True(tb, ok)
			layers[i] = layer.Interface().(types.LayerID)
		}

		st.Last = layers[0]
		st.Verified = layers[1]
		st.LastEvicted = layers[2]

		st.GoodBlocksIndex = map[types.BlockID]bool{}
		st.BlockOpinionsByLayer = map[types.LayerID]map[types.BlockID]Opinion{}

		for i := 0; i < 200; i++ {
			layerGen, ok := quick.Value(reflect.TypeOf(types.LayerID{}), rng)
			require.True(tb, ok)
			block1Gen, ok := quick.Value(reflect.TypeOf(types.BlockID{}), rng)
			require.True(tb, ok)
			block2Gen, ok := quick.Value(reflect.TypeOf(types.BlockID{}), rng)
			require.True(tb, ok)

			layer := layerGen.Interface().(types.LayerID)
			if _, exist := st.BlockOpinionsByLayer[layer]; !exist {
				st.BlockOpinionsByLayer[layer] = map[types.BlockID]Opinion{}
			}
			block1 := block1Gen.Interface().(types.BlockID)
			if _, exist := st.BlockOpinionsByLayer[layer][block1]; !exist {
				st.BlockOpinionsByLayer[layer][block1] = Opinion{}
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
		st.LastEvicted = layers[len(layers)/2]
		if !assert.NoError(t, st.Persist()) {
			return false
		}

		for layer := range st.BlockOpinionsByLayer {
			if layer.After(st.LastEvicted) {
				continue
			}
			for block := range st.BlockOpinionsByLayer[layer] {
				delete(st.GoodBlocksIndex, block)
			}
			delete(st.BlockOpinionsByLayer, layer)
		}
		if !assert.NoError(t, st.Evict()) {
			return false
		}

		original := *st
		st.BlockOpinionsByLayer = nil
		st.GoodBlocksIndex = nil
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
