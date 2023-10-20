package cache

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"runtime"
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestCache(t *testing.T) {
	t.Run("sanity", func(t *testing.T) {
		const (
			epochs = 10
			ids    = 100
		)
		c := New()
		f := fuzz.New()
		f.RandSource(rand.NewSource(101))
		nodes := [epochs][ids]types.NodeID{}
		atxids := [epochs][ids]types.ATXID{}
		data := [epochs][ids]ATXData{}
		f.Fuzz(&data)

		for repeat := 0; repeat < 10; repeat++ {
			for epoch := 0; epoch < epochs; epoch++ {
				for i := range nodes[epoch] {
					nodes[epoch][i][0] = byte(epoch + i)
					atxids[epoch][i][0] = byte(epoch + i)
					c.Add(types.EpochID(epoch)+1, nodes[epoch][i], atxids[epoch][i], &data[epoch][i])
				}
			}
			for epoch := 0; epoch < epochs; epoch++ {
				for i := range nodes[epoch] {
					bynode := c.GetByNode(types.EpochID(epoch)+1, nodes[epoch][i])
					require.Equal(t, &data[epoch][i], bynode)
					byatxid := c.Get(types.EpochID(epoch)+1, nodes[epoch][i], atxids[epoch][i])
					require.Equal(t, &data[epoch][i], byatxid)
				}
			}
		}
	})
	t.Run("malicious", func(t *testing.T) {
		c := New()
		node := types.NodeID{1}
		for epoch := 1; epoch <= 10; epoch++ {
			c.Add(types.EpochID(epoch), node, types.ATXID{}, &ATXData{})
			data := c.GetByNode(types.EpochID(epoch), node)
			require.NotNil(t, data)
			require.False(t, data.Malicious)
		}
		c.SetMalicious(node)
		for epoch := 1; epoch <= 10; epoch++ {
			data := c.GetByNode(types.EpochID(epoch), node)
			require.True(t, data.Malicious)
		}
	})
	t.Run("eviction", func(t *testing.T) {
		const (
			epochs   = 10
			capacity = 3
			applied  = epochs / 2
		)
		c := New(WithCapacity(capacity))
		node := types.NodeID{1}
		for epoch := 1; epoch <= epochs; epoch++ {
			c.Add(types.EpochID(epoch), node, types.ATXID{}, &ATXData{})
			data := c.GetByNode(types.EpochID(epoch), node)
			require.NotNil(t, data)
		}
		c.OnEpoch(applied)
		evicted := applied - capacity
		require.EqualValues(t, evicted, c.Evicted())
		for epoch := 1; epoch <= epochs; epoch++ {
			require.Equal(t, epoch <= evicted, c.IsEvicted(types.EpochID(epoch)), "epoch=%v", epoch)
		}
		for epoch := 1; epoch <= evicted; epoch++ {
			data := c.GetByNode(types.EpochID(epoch), node)
			require.Nil(t, data)
		}
	})
	t.Run("nil responses", func(t *testing.T) {
		c := New()
		require.Nil(t, c.Get(0, types.NodeID{}, types.ATXID{}))
		require.Nil(t, c.Get(1, types.NodeID{}, types.ATXID{}))
		require.Nil(t, c.GetByNode(1, types.NodeID{}))

		c.Add(1, types.NodeID{1}, types.ATXID{1}, &ATXData{})
		require.Nil(t, c.Get(1, types.NodeID{}, types.ATXID{}))
		require.Nil(t, c.GetByNode(1, types.NodeID{2}))
	})
	t.Run("multiple atxs", func(t *testing.T) {
		c := New()
		c.Add(1, types.NodeID{1}, types.ATXID{1}, &ATXData{Weight: 1})
		c.Add(1, types.NodeID{1}, types.ATXID{2}, &ATXData{Weight: 2})
		require.NotNil(t, c.Get(1, types.NodeID{1}, types.ATXID{1}))
		require.NotNil(t, c.Get(1, types.NodeID{1}, types.ATXID{2}))
		require.EqualValues(t, 1, c.GetByNode(1, types.NodeID{1}).Weight)
	})
	t.Run("weight for set", func(t *testing.T) {
		c := New()
		c.Add(1, types.NodeID{1}, types.ATXID{1}, &ATXData{Weight: 1})
		c.Add(1, types.NodeID{1}, types.ATXID{2}, &ATXData{Weight: 2})

		weight, used := c.WeightForSet(1, []types.ATXID{{1}, {2}, {3}})
		require.Equal(t, []bool{true, true, false}, used)
		require.EqualValues(t, 3, weight)

		weight, used = c.WeightForSet(1, []types.ATXID{{1}})
		require.Equal(t, []bool{true}, used)
		require.EqualValues(t, 1, weight)
	})
	t.Run("adding after eviction", func(t *testing.T) {
		c := New()
		c.OnEpoch(0)
		c.OnEpoch(3)
		c.Add(1, types.NodeID{1}, types.ATXID{1}, &ATXData{})
		require.Nil(t, c.Get(3, types.NodeID{1}, types.ATXID{1}))
		c.OnEpoch(3)
	})
}

func TestMemory(t *testing.T) {
	test := func(t *testing.T, size int, memory, delta uint64) {
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		c := New()
		for i := 1; i <= size; i++ {
			var (
				node types.NodeID
				atx  types.ATXID
				data = &ATXData{Weight: 500, BaseHeight: 100}
			)
			binary.PutUvarint(node[:], uint64(i))
			binary.PutUvarint(atx[:], uint64(i))
			c.Add(1, node, atx, data)
		}
		runtime.GC()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		require.InDelta(t, after.HeapInuse-before.HeapInuse, memory, float64(delta))

		c.OnEpoch(0) // otherwise cache will be gc'ed
	}
	t.Run("1_000_000", func(t *testing.T) {
		test(t, 1_000_000, 137_682_944, 300_000)
	})
	t.Run("100_000", func(t *testing.T) {
		test(t, 100_000, 14_147_584, 200_000)
	})
}

func BenchmarkConcurrentReadWrite(b *testing.B) {
	c := New()
	const (
		epoch = 1
		size  = 1_000_000
	)
	for i := 1; i <= size; i++ {
		var (
			node types.NodeID
			atx  types.ATXID
			data = &ATXData{Weight: 500, BaseHeight: 100}
		)
		binary.PutUvarint(node[:], uint64(i))
		binary.PutUvarint(atx[:], uint64(i))
		c.Add(epoch, node, atx, data)
	}
	b.ResetTimer()

	var parallel atomic.Uint64
	const writeFraction = 10
	b.RunParallel(func(pb *testing.PB) {
		var i uint64
		core := parallel.Add(1)
		var (
			node types.NodeID
			atx  types.ATXID
		)
		for pb.Next() {
			if i%writeFraction == 0 {
				binary.PutUvarint(node[:], size+i*core)
				binary.PutUvarint(atx[:], size+i*core)
				c.Add(epoch, node, atx, &ATXData{})
			} else {
				binary.PutUvarint(node[:], i%size)
				binary.PutUvarint(atx[:], i%size)
				_ = c.Get(epoch, node, atx)
			}
			i++
		}
	})
}

func benchmarkkWeightForSet(b *testing.B, size, setSize int) {
	c := New()
	const epoch = 1
	atxs := make([]types.ATXID, 0, size)
	rng := rand.New(rand.NewSource(10101))
	for i := 1; i <= size; i++ {
		var (
			node types.NodeID
			atx  types.ATXID
			data = &ATXData{Weight: 500, BaseHeight: 100}
		)
		binary.PutUvarint(node[:], uint64(i))
		binary.PutUvarint(atx[:], uint64(i))
		atxs = append(atxs, atx)
		c.Add(epoch, node, atx, data)
	}
	rng.Shuffle(size, func(i, j int) {
		atxs[i], atxs[j] = atxs[j], atxs[i]
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		weight, used := c.WeightForSet(epoch, atxs[:setSize])
		if weight == 0 {
			b.Fatalf("weight can't be zero")
		}
		if len(used) != setSize {
			b.Fatalf("used should be equal to set size")
		}
	}
}

func BenchmarkWeightForSet(b *testing.B) {
	for _, bc := range []struct {
		size, setSize int
	}{
		{100_000, 100_000},
		{200_000, 200_000},
		{400_000, 400_000},
		{1_000_000, 100_000},
		{1_000_000, 200_000},
		{1_000_000, 400_000},
		{1_000_000, 1_000_000},
	} {
		bc := bc
		b.Run(fmt.Sprintf("size=%d set_size=%d", bc.size, bc.setSize), func(b *testing.B) {
			benchmarkkWeightForSet(b, bc.size, bc.setSize)
		})
	}
}
