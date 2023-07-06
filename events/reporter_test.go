package events

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRingBuffer(t *testing.T) {
	const cap = 10

	t.Run("empty", func(t *testing.T) {
		buffer := newRing[int](cap)
		require.Equal(t, cap, buffer.cap())
		buffer.iterate(func(val int) bool {
			require.Fail(t, "should not be called")
			return true
		})
	})

	t.Run("overwrite", func(t *testing.T) {
		buffer := newRing[int](cap)
		for i := 0; i < cap*2; i++ {
			buffer.insert(i)
		}
		expect := cap
		buffer.iterate(func(val int) bool {
			require.Equal(t, expect, val)
			expect++
			return true
		})
		require.Equal(t, cap*2, expect)
	})
	t.Run("terminate", func(t *testing.T) {
		buffer := newRing[int](cap)
		for i := 0; i < cap; i++ {
			buffer.insert(i)
		}
		expect := 0
		terminate := cap / 2
		buffer.iterate(func(val int) bool {
			require.Equal(t, expect, val)
			require.Less(t, val, terminate)
			expect++
			return expect < terminate
		})
		require.Equal(t, terminate, expect)
	})
}
