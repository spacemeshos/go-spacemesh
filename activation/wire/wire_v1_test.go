package wire

import (
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/require"

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
