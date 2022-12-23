package types

import "testing"

func BenchmarkSafeMul(b *testing.B) {
	var c uint64

	for i := 0; i < b.N; i++ {
		int1 := uint64(i * 2)
		int2 := uint64(i + 5)

		c = safeMul(int1, int2)
	}

	_ = c // avoid compiler optimizations
}

func BenchmarkMul(b *testing.B) {
	var c uint64

	for i := 0; i < b.N; i++ {
		int1 := uint64(i * 2)
		int2 := uint64(i + 5)

		c = unsafeMul(int1, int2)
	}

	_ = c // avoid compiler optimizations
}

func unsafeMul(x, y uint64) uint64 {
	_ = x // avoid compiler optimizations
	return x * y
}
