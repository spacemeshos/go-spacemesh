package wire

import (
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func Benchmark_ATXv1ID(b *testing.B) {
	f := fuzz.New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		atx := &ActivationTxV1{}
		f.Fuzz(atx)
		b.StartTimer()
		atx.ID()
	}
}

func Test_NoATXv1IDCollisions(t *testing.T) {
	f := fuzz.New()

	atxIDs := make([]types.ATXID, 0, 1000)
	for range 1000 {
		atx := &ActivationTxV1{}
		f.Fuzz(atx)
		id := atx.ID()
		require.NotContains(t, atxIDs, id, "ATX ID collision")
		atxIDs = append(atxIDs, id)
	}
}

func Fuzz_ATXv1IDConsistency(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzer := fuzz.NewFromGoFuzz(data)
		atx := &ActivationTxV1{}
		fuzzer.Fuzz(atx)
		id := atx.ID()
		encoded := codec.MustEncode(atx)
		decoded := &ActivationTxV1{}
		codec.MustDecode(encoded, decoded)
		require.Equal(t, id, atx.ID(), "ID should be consistent")
	})
}
