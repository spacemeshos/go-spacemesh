package wire

import (
	"fmt"
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func Benchmark_ATXv2ID(b *testing.B) {
	f := fuzz.New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		atx := &ActivationTxV2{}
		f.Fuzz(atx)
		b.StartTimer()
		atx.ID()
	}
}

func Benchmark_ATXv2ID_WorstScenario(b *testing.B) {
	atx := &ActivationTxV2{
		PublishEpoch:   0,
		PositioningATX: types.RandomATXID(),
		PreviousATXs:   make([]types.ATXID, 256),
		NIPosts: []NIPostV2{
			{
				Membership: MerkleProofV2{
					Nodes: make([]types.Hash32, 32),
				},
				Challenge: types.RandomHash(),
				Posts:     make([]SubPostV2, 256),
			},
			{
				Membership: MerkleProofV2{
					Nodes: make([]types.Hash32, 32),
				},
				Challenge: types.RandomHash(),
				Posts:     make([]SubPostV2, 256), // actually the sum of all posts in `NiPosts` should be 256
			},
			{
				Membership: MerkleProofV2{
					Nodes: make([]types.Hash32, 32),
				},
				Challenge: types.RandomHash(),
				Posts:     make([]SubPostV2, 256), // actually the sum of all posts in `NiPosts` should be 256
			},
			{
				Membership: MerkleProofV2{
					Nodes: make([]types.Hash32, 32),
				},
				Challenge: types.RandomHash(),
				Posts:     make([]SubPostV2, 256), // actually the sum of all posts in `NiPosts` should be 256
			},
		},
	}
	for j := range atx.NIPosts {
		for i := range atx.NIPosts[j].Posts {
			atx.NIPosts[j].Posts[i].Post = PostV1{
				Nonce:   0,
				Indices: make([]byte, 800),
				Pow:     0,
			}
		}
	}
	atx.MarriageATX = new(types.ATXID)

	var id types.ATXID
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		atx.id = types.EmptyATXID
		id = atx.ID()
	}
	require.Equal(b, id, atx.ID())
}

func Test_NoATXv2IDCollisions(t *testing.T) {
	f := fuzz.New()

	atxIDs := make([]types.ATXID, 0, 1000)
	for range 1000 {
		atx := &ActivationTxV2{}
		f.Fuzz(atx)
		id := atx.ID()
		require.NotContains(t, atxIDs, id, "ATX ID collision")
		atxIDs = append(atxIDs, id)
	}
}

func Fuzz_ATXv2IDConsistency(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzer := fuzz.NewFromGoFuzz(data).
			// Ensure that `NIPosts` is at most 4 elements long
			Funcs(func(niposts *NIPosts, c fuzz.Continue) {
				*niposts = make([]NIPostV2, c.Intn(5))
				for i := range *niposts {
					c.Fuzz(&(*niposts)[i])
				}
			})
		atx := &ActivationTxV2{}
		fuzzer.Fuzz(atx)
		id := atx.ID()
		encoded := codec.MustEncode(atx)
		decoded := &ActivationTxV2{}
		codec.MustDecode(encoded, decoded)
		require.Equal(t, id, atx.ID(), "ID should be consistent")
	})
}

func Test_ATXv2_SupportUpTo4Niposts(t *testing.T) {
	f := fuzz.New()
	atx := &ActivationTxV2{}
	f.Fuzz(atx)
	for i := range 4 {
		t.Run(fmt.Sprintf("supports %d poet", i), func(t *testing.T) {
			atx.NIPosts = make([]NIPostV2, i)
			_, err := codec.Encode(atx)
			require.NoError(t, err)
		})
	}
	t.Run("doesn't support > 5 niposts", func(t *testing.T) {
		atx.NIPosts = make([]NIPostV2, 5)
		_, err := codec.Encode(atx)
		require.Error(t, err)
	})
}
