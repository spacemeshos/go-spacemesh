package atxcache_test

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"sync/atomic"
	"testing"
	"time"

	fuzz "github.com/google/gofuzz"
	"github.com/spacemeshos/go-spacemesh/atxcache"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/pebble"
	"github.com/stretchr/testify/require"
)

func TestCache(t *testing.T) {
}

func TestData(t *testing.T) {
	t.Run("sanity", func(t *testing.T) {
		const (
			epochs = 10
			ids    = 100
		)
		c := atxcache.New(testPebble(t))
		f := fuzz.New()
		f.RandSource(rand.NewSource(101))
		atxids := [epochs][ids]types.ATXID{}
		data := [epochs][ids]atxcache.ATX{}
		f.Fuzz(&data)

		for repeat := 0; repeat < 10; repeat++ {
			for epoch := 0; epoch < epochs; epoch++ {
				for i := range atxids[epoch] {
					atxids[epoch][i] = types.ATXID{byte(epoch), byte(i)}
					data[epoch][i].Node = types.NodeID{byte(epoch), byte(i)}
					d := data[epoch][i]
					c.Add(
						atxids[epoch][i],
						d.Node,
						types.EpochID(epoch)+1,
						d.Coinbase,
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
					require.False(t, c.IsMalicious(atx.Node))
				}
			}
		}
	})
	t.Run("malicious", func(t *testing.T) {
		c := atxcache.New(testPebble(t))
		node := types.NodeID{1}
		for epoch := 1; epoch <= 10; epoch++ {
			c.Add(
				types.ATXID{byte(epoch)},
				node,
				types.EpochID(epoch),
				types.Address{},
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
		c := atxcache.New(testPebble(t))
		node := types.NodeID{1}
		for epoch := 1; epoch <= epochs; epoch++ {
			c.Add(types.ATXID{}, node, types.EpochID(epoch), types.Address{}, 2, 0, 0, 0, false)
			data := c.Get(types.EpochID(epoch), types.ATXID{})
			require.NotNil(t, data)
		}

		evicted := applied - capacity
		c.EvictEpoch(types.EpochID(evicted))
		require.EqualValues(t, evicted, c.Evicted())
		for epoch := 1; epoch <= epochs; epoch++ {
			fmt.Println(epoch, evicted)
			require.Equal(t, epoch <= evicted, c.IsEvicted(types.EpochID(epoch)), "epoch=%v", epoch)
		}
	})
	t.Run("nil responses", func(t *testing.T) {
		c := atxcache.New(testPebble(t))
		require.Nil(t, c.Get(0, types.ATXID{}))
		require.Nil(t, c.Get(1, types.ATXID{}))

		c.Add(types.ATXID{1}, types.NodeID{1}, 1, types.Address{}, 2, 0, 0, 0, false)
		require.Nil(t, c.Get(1, types.ATXID{}))
	})
	t.Run("multiple atxs", func(t *testing.T) {
		c := atxcache.New(testPebble(t))
		c.Add(types.ATXID{1}, types.NodeID{1}, 1, types.Address{}, 1, 0, 0, 0, false)
		c.Add(types.ATXID{2}, types.NodeID{1}, 1, types.Address{}, 2, 0, 0, 0, false)
		require.NotNil(t, c.Get(1, types.ATXID{1}))
		require.NotNil(t, c.Get(1, types.ATXID{2}))
	})
	t.Run("weight for set", func(t *testing.T) {
		c := atxcache.New(testPebble(t))
		c.Add(types.ATXID{1}, types.NodeID{1}, 1, types.Address{}, 1, 0, 0, 0, false)
		c.Add(types.ATXID{2}, types.NodeID{1}, 1, types.Address{}, 2, 0, 0, 0, false)

		weight, used := c.WeightForSet(1, []types.ATXID{{1}, {2}, {3}})
		require.Equal(t, []bool{true, true, false}, used)
		require.EqualValues(t, 3, weight)

		weight, used = c.WeightForSet(1, []types.ATXID{{1}})
		require.Equal(t, []bool{true}, used)
		require.EqualValues(t, 1, weight)
	})
	t.Run("adding after eviction", func(t *testing.T) {
		c := atxcache.New(testPebble(t))
		c.EvictEpoch(0)
		c.EvictEpoch(3)
		c.Add(types.ATXID{1}, types.NodeID{1}, 1, types.Address{}, 500, 100, 0, 0, false)
		require.Nil(t, c.Get(3, types.ATXID{1}))
		c.EvictEpoch(3)
	})
}

func TestAtxMarshal(t *testing.T) {
	a := atxcache.ATX{
		Node:       types.RandomNodeID(),
		Coinbase:   types.Address(types.RandomBytes(24)),
		Weight:     uint64(99999999),
		BaseHeight: uint64(9999387373),
		Height:     uint64(8383377322),
		Nonce:      types.VRFPostIndex(1232131),
	}

	ab, _ := a.MarshalBinary()
	ac := new(atxcache.ATX)
	ac.UnmarshalBinary(ab)
	require.Equal(t, a.Node, ac.Node)
	require.Equal(t, a.Coinbase, ac.Coinbase)
	require.Equal(t, a.Weight, ac.Weight)
	require.Equal(t, a.BaseHeight, ac.BaseHeight)
	require.Equal(t, a.Height, ac.Height)
	require.Equal(t, a.Nonce, ac.Nonce)
}

func testPebble(tb testing.TB) *pebble.KvDb {
	peb := pebble.New(tb.TempDir())
	tb.Cleanup(func() { peb.Close() })
	return peb
}

func BenchmarkConcurrentReadWrite(b *testing.B) {
	c := atxcache.New(testPebble(b))
	const (
		epoch = 1
		size  = 1_000_000
	)
	start := time.Now()
	for i := range size {
		var (
			node types.NodeID
			atx  types.ATXID
		)
		binary.PutUvarint(node[:], uint64(i+1))
		binary.PutUvarint(atx[:], uint64(i+1))
		c.FastAdd(atx, node, epoch, types.Address{}, 500, 100, 0, 0, false)
	}
	fmt.Println("finished inserting 1mil entries, took", time.Now().Sub(start))
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
				c.Add(atx, node, epoch, types.Address{}, 500, 100, 0, 0, false)
			} else {
				binary.PutUvarint(node[:], i%size)
				binary.PutUvarint(atx[:], i%size)
				_ = c.Get(epoch, atx)
			}
			i++
		}
	})
}

func benchmarkkWeightForSet(b *testing.B, size, setSize int) {
	c := atxcache.New(testPebble(b))
	const epoch = 1
	atxs := make([]types.ATXID, 0, size)
	rng := rand.New(rand.NewSource(10101))
	for i := range size {
		var (
			node types.NodeID
			atx  types.ATXID
		)
		binary.PutUvarint(node[:], uint64(i+1))
		binary.PutUvarint(atx[:], uint64(i+1))
		atxs = append(atxs, atx)
		c.FastAdd(atx, node, epoch, types.Address{}, 500, 100, 0, 0, false)
	}
	rng.Shuffle(size, func(i, j int) {
		atxs[i], atxs[j] = atxs[j], atxs[i]
	})
	b.ResetTimer()
	for range b.N {
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
		b.Run(fmt.Sprintf("size=%d set_size=%d", bc.size, bc.setSize), func(b *testing.B) {
			benchmarkkWeightForSet(b, bc.size, bc.setSize)
		})
	}
}
