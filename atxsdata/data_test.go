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
)

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

func benchmarkkWeightForSet(b *testing.B, size, setSize int) {
	c := New()
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
		c.Add(epoch, node, types.Address{}, atx, 500, 100, 0, 0, false)
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
