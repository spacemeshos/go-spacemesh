package atxsdata

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestData(t *testing.T) {
	t.Run("sanity", func(t *testing.T) {
		const (
			epochs = 10
			ids    = 100
		)
		c := New()
		atxids := [epochs][ids]types.ATXID{}
		data := [epochs][ids]ATX{}

		for repeat := 0; repeat < 10; repeat++ {
			for epoch := 0; epoch < epochs; epoch++ {
				for i := range atxids[epoch] {
					atxids[epoch][i] = types.ATXID{byte(epoch), byte(i)}
					data[epoch][i].Node = types.NodeID{byte(epoch), byte(i)}
					d := data[epoch][i]
					c.Add(
						types.EpochID(epoch)+1,
						d.Node,
						d.Coinbase,
						atxids[epoch][i],
						d.Weight,
						d.BaseHeight,
						d.Height,
						d.Nonce,
						false,
					)
				}
			}
			for epoch := 0; epoch < epochs; epoch++ {
				for i := range atxids[epoch] {
					atx := c.Get(types.EpochID(epoch)+1, atxids[epoch][i])
					require.Equal(t, &data[epoch][i], atx)
					id, atx := c.GetByEpochAndNodeID(types.EpochID(epoch)+1, data[epoch][i].Node)
					require.Equal(t, atxids[epoch][i], id)
					require.Equal(t, &data[epoch][i], atx)
					require.False(t, c.IsMalicious(atx.Node))
				}
			}
		}
	})
	t.Run("managed", func(t *testing.T) {
		const (
			epochs = 10
			ids    = 100
		)
		c := New()
		atxids := [epochs][ids]types.ATXID{}
		data := [epochs][ids]ATX{}
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		c.Register(sig)

		for repeat := 0; repeat < 10; repeat++ {
			for epoch := 0; epoch < epochs; epoch++ {
				atxids[epoch][0] = types.ATXID{byte(epoch), 0}
				data[epoch][0].Node = sig.NodeID()
				d := data[epoch][0]
				c.Add(
					types.EpochID(epoch)+1,
					d.Node,
					d.Coinbase,
					atxids[epoch][0],
					d.Weight,
					d.BaseHeight,
					d.Height,
					d.Nonce,
					false,
				)
				for i := 1; i < len(atxids[epoch]); i++ {
					atxids[epoch][i] = types.ATXID{byte(epoch), byte(i)}
					data[epoch][i].Node = types.NodeID{byte(epoch), byte(i)}
					d := data[epoch][i]
					c.Add(
						types.EpochID(epoch)+1,
						d.Node,
						d.Coinbase,
						atxids[epoch][i],
						d.Weight,
						d.BaseHeight,
						d.Height,
						d.Nonce,
						false,
					)
				}
			}
			for epoch := 0; epoch < epochs; epoch++ {
				for i := range atxids[epoch] {
					atx := c.Get(types.EpochID(epoch)+1, atxids[epoch][i])
					require.Equal(t, &data[epoch][i], atx)
					id, atx := c.GetByEpochAndNodeID(types.EpochID(epoch)+1, data[epoch][i].Node)
					require.Equal(t, atxids[epoch][i], id)
					require.Equal(t, &data[epoch][i], atx)
					require.False(t, c.IsMalicious(atx.Node))
				}
			}
		}
	})
	t.Run("malicious", func(t *testing.T) {
		c := New()
		node := types.NodeID{1}
		for epoch := 1; epoch <= 10; epoch++ {
			c.Add(
				types.EpochID(epoch),
				node,
				types.Address{},
				types.ATXID{byte(epoch)},
				2,
				0,
				0,
				0,
				false,
			)
			data := c.Get(types.EpochID(epoch), types.ATXID{byte(epoch)})
			require.NotNil(t, data)
			require.False(t, c.IsMalicious(data.Node))
		}
		c.SetMalicious(node)
		require.True(t, c.IsMalicious(node))
	})
	t.Run("eviction", func(t *testing.T) {
		const (
			epochs   = 10
			capacity = 3
			applied  = epochs / 2
		)
		c := New()
		node := types.NodeID{1}
		for epoch := 1; epoch <= epochs; epoch++ {
			c.Add(types.EpochID(epoch), node, types.Address{}, types.ATXID{}, 2, 0, 0, 0, false)
			data := c.Get(types.EpochID(epoch), types.ATXID{})
			require.NotNil(t, data)
		}

		evicted := applied - capacity
		c.EvictEpoch(types.EpochID(evicted))
		require.EqualValues(t, evicted, c.Evicted())
		for epoch := 1; epoch <= epochs; epoch++ {
			require.Equal(t, epoch <= evicted, c.IsEvicted(types.EpochID(epoch)), "epoch=%v", epoch)
		}
	})
	t.Run("nil responses", func(t *testing.T) {
		c := New()
		require.Nil(t, c.Get(0, types.ATXID{}))
		require.Nil(t, c.Get(1, types.ATXID{}))

		c.Add(1, types.NodeID{1}, types.Address{}, types.ATXID{1}, 2, 0, 0, 0, false)
		require.Nil(t, c.Get(1, types.ATXID{}))
	})
	t.Run("multiple atxs", func(t *testing.T) {
		c := New()
		c.Add(1, types.NodeID{1}, types.Address{}, types.ATXID{1}, 1, 0, 0, 0, false)
		c.Add(1, types.NodeID{1}, types.Address{}, types.ATXID{2}, 2, 0, 0, 0, false)
		require.NotNil(t, c.Get(1, types.ATXID{1}))
		require.NotNil(t, c.Get(1, types.ATXID{2}))
	})
	t.Run("weight for set", func(t *testing.T) {
		c := New()
		c.Add(1, types.NodeID{1}, types.Address{}, types.ATXID{1}, 1, 0, 0, 0, false)
		c.Add(1, types.NodeID{1}, types.Address{}, types.ATXID{2}, 2, 0, 0, 0, false)

		weight, used := c.WeightForSet(1, []types.ATXID{{1}, {2}, {3}})
		require.Equal(t, []bool{true, true, false}, used)
		require.EqualValues(t, 3, weight)

		weight, used = c.WeightForSet(1, []types.ATXID{{1}})
		require.Equal(t, []bool{true}, used)
		require.EqualValues(t, 1, weight)
	})
	t.Run("adding after eviction", func(t *testing.T) {
		c := New()
		c.EvictEpoch(0)
		c.EvictEpoch(3)
		c.Add(1, types.NodeID{1}, types.Address{}, types.ATXID{1}, 500, 100, 0, 0, false)
		require.Nil(t, c.Get(3, types.ATXID{1}))
		c.EvictEpoch(3)
	})
}

func TestMemory(t *testing.T) {
	t.Skip("memory layouts can change from one go version to the next and might differ on different architectures")

	test := func(t *testing.T, size, memory, delta uint64) {
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		c := New()
		for i := range size {
			var (
				node types.NodeID
				atx  types.ATXID
			)
			binary.PutUvarint(node[:], uint64(i+1))
			binary.PutUvarint(atx[:], uint64(i+1))
			c.Add(1, node, types.Address{}, atx, 500, 100, 0, 0, false)
		}
		runtime.GC()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		require.InDelta(t, after.HeapInuse-before.HeapInuse, memory, float64(delta))

		c.EvictEpoch(0) // otherwise cache will be gc'ed
	}
	t.Run("1_000_000", func(t *testing.T) {
		test(t, 1_000_000, 189_956_096, 300_000)
	})
	t.Run("100_000", func(t *testing.T) {
		test(t, 100_000, 16_080_896, 200_000)
	})
}

func BenchmarkConcurrentReadWrite(b *testing.B) {
	c := New()
	const (
		epoch = 1
		size  = 1_000_000
	)
	for i := range size {
		var (
			node types.NodeID
			atx  types.ATXID
		)
		binary.PutUvarint(node[:], uint64(i+1))
		binary.PutUvarint(atx[:], uint64(i+1))
		c.Add(epoch, node, types.Address{}, atx, 500, 100, 0, 0, false)
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
				c.Add(epoch, node, types.Address{}, atx, 500, 100, 0, 0, false)
			} else {
				binary.PutUvarint(node[:], i%size)
				binary.PutUvarint(atx[:], i%size)
				_ = c.Get(epoch, atx)
			}
			i++
		}
	})
}

func benchmarkCache(b *testing.B, size int, sigs ...*signing.EdSigner) (*Data, types.EpochID, []types.ATXID) {
	c := New()
	const epoch = 1
	nodeIDs := make([]types.NodeID, 0, size)
	atxs := make([]types.ATXID, 0, size)
	rng := rand.New(rand.NewSource(10101))
	for i := range size {
		var (
			nodeID types.NodeID
			atxID  types.ATXID
		)
		binary.PutUvarint(nodeID[:], uint64(i+1))
		nodeIDs = append(nodeIDs, nodeID)
		binary.PutUvarint(atxID[:], uint64(i+1))
		atxs = append(atxs, atxID)
	}
	for i, sig := range sigs {
		c.Register(sig)
		nodeIDs[i] = sig.NodeID()
	}
	for i := range size {
		c.Add(epoch, nodeIDs[i], types.Address{}, atxs[i], 500, 100, 0, 0, false)
	}
	rng.Shuffle(size, func(i, j int) {
		atxs[i], atxs[j] = atxs[j], atxs[i]
	})
	b.ResetTimer()
	return c, epoch, atxs
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
		b.Run(fmt.Sprintf("size=%d set_size=%d", bc.size, bc.setSize), func(b *testing.B) {
			c, epoch, atxs := benchmarkCache(b, bc.size)

			b.ResetTimer()
			for range b.N {
				weight, used := c.WeightForSet(epoch, atxs[:bc.setSize])
				if weight == 0 {
					b.Fatalf("weight can't be zero")
				}
				if len(used) != bc.setSize {
					b.Fatalf("used should be equal to set size")
				}
			}
		})
	}
}

func BenchmarkGetByNodeID(b *testing.B) {
	for _, bc := range []struct {
		size int
	}{
		{100_000},
		{200_000},
		{400_000},
		{1_000_000},
	} {
		b.Run(fmt.Sprintf("size=%d", bc.size), func(b *testing.B) {
			c, epoch, _ := benchmarkCache(b, bc.size)
			var nodeID types.NodeID
			binary.PutUvarint(nodeID[:], uint64(10+1))

			var id types.ATXID
			var atx *ATX
			b.ResetTimer()
			for range b.N {
				id, atx = c.GetByEpochAndNodeID(epoch, nodeID)
			}
			require.NotEqual(b, types.EmptyATXID, id)
			require.NotNil(b, atx)
		})
	}
}

func BenchmarkGetByNodeID_Managed(b *testing.B) {
	for _, bc := range []struct {
		size int
	}{
		{100_000},
		{200_000},
		{400_000},
		{1_000_000},
	} {
		b.Run(fmt.Sprintf("size=%d", bc.size), func(b *testing.B) {
			sig, err := signing.NewEdSigner()
			require.NoError(b, err)

			c, epoch, _ := benchmarkCache(b, bc.size, sig)
			nodeID := sig.NodeID()

			var id types.ATXID
			var atx *ATX
			b.ResetTimer()
			for range b.N {
				id, atx = c.GetByEpochAndNodeID(epoch, nodeID)
			}
			require.NotEqual(b, types.EmptyATXID, id)
			require.NotNil(b, atx)
		})
	}
}

func TestFindHighestHonest(t *testing.T) {
	t.Parallel()

	t.Run("empty data returns empty ATXID", func(t *testing.T) {
		data := New()
		id := data.FindHighestHonest()
		require.Equal(t, types.EmptyATXID, id)
	})

	t.Run("returns highest ATX from most recent epoch", func(t *testing.T) {
		data := New()

		// Add ATXs to epoch 1
		atx1ID := types.ATXID{1}
		atx1 := &ATX{
			Node:   types.NodeID{1},
			Height: 100,
		}
		data.AddAtx(1, atx1ID, atx1)

		// Add ATXs to epoch 2 (more recent)
		atx2ID := types.ATXID{2}
		atx2 := &ATX{
			Node:   types.NodeID{2},
			Height: 50, // Lower height
		}
		data.AddAtx(2, atx2ID, atx2)

		atx3ID := types.ATXID{3}
		atx3 := &ATX{
			Node:   types.NodeID{3},
			Height: 150, // Higher height
		}
		data.AddAtx(2, atx3ID, atx3)

		// Should return the highest ATX from epoch 2
		id := data.FindHighestHonest()
		require.Equal(t, atx3ID, id)
	})

	t.Run("ignores malicious ATXs", func(t *testing.T) {
		data := New()

		// Add honest ATX to epoch 2
		honestID := types.ATXID{1}
		honest := &ATX{
			Node:   types.NodeID{1},
			Height: 100,
		}
		data.AddAtx(2, honestID, honest)

		// Add malicious ATX to epoch 2 with higher height
		maliciousID := types.ATXID{2}
		malicious := &ATX{
			Node:   types.NodeID{2},
			Height: 200, // Higher height but malicious
		}
		data.AddAtx(2, maliciousID, malicious)
		data.SetMalicious(malicious.Node)

		// Should return the honest ATX despite lower height
		id := data.FindHighestHonest()
		require.Equal(t, honestID, id)
	})

	t.Run("handles more than two epochs correctly", func(t *testing.T) {
		data := New()

		// Add ATXs to multiple epochs
		for epoch := types.EpochID(1); epoch <= 5; epoch++ {
			atxID := types.ATXID{byte(epoch)}
			atx := &ATX{
				Node:   types.NodeID{byte(epoch)},
				Height: uint64(epoch * 100), // Height increases with epoch
			}
			data.AddAtx(epoch, atxID, atx)
		}

		// Should consider only epochs 4 and 5, and return the highest from those
		id := data.FindHighestHonest()

		// Highest should be from epoch 5
		expectedID := types.ATXID{5}
		require.Equal(t, expectedID, id)
	})

	t.Run("returns ATX from the most recent epoch when multiple ATXs have same height", func(t *testing.T) {
		data := New()

		// Add multiple ATXs with same height to the same epoch
		atx1ID := types.ATXID{1}
		atx1 := &ATX{
			Node:   types.NodeID{1},
			Height: 100,
		}
		data.AddAtx(2, atx1ID, atx1)

		atx2ID := types.ATXID{2}
		atx2 := &ATX{
			Node:   types.NodeID{2},
			Height: 100, // Same height
		}
		data.AddAtx(3, atx2ID, atx2)

		// The method should return one of them (implementation dependent)
		id := data.FindHighestHonest()
		require.Equal(t, atx2ID, id)
	})
}
