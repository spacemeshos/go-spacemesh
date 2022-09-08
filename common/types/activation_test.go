package types

import (
	"math/rand"
	"testing"
)

var result uint64

func BenchmarkSafeMul(b *testing.B) {
	var c uint64

	for i := 0; i < b.N; i++ {
		int1 := uint64(rand.Int31())
		int2 := uint64(rand.Int31())

		c = safeMul(int1, int2)
	}

	result = c // avoid compiler optimizations
}

func BenchmarkMul(b *testing.B) {
	var c uint64

	for i := 0; i < b.N; i++ {
		int1 := uint64(rand.Int31())
		int2 := uint64(rand.Int31())

		c = unsafeMul(int1, int2)
	}

	result = c // avoid compiler optimizations
}

func unsafeMul(x, y uint64) uint64 {
	result = x // avoid compiler optimizations
	return x * y
}
