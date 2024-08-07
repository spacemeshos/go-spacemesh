package wire

import (
	"math/rand/v2"
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type testAtxV2Opt func(*ActivationTxV2)

func withPublishEpoch(epoch types.EpochID) testAtxV2Opt {
	return func(atx *ActivationTxV2) {
		atx.PublishEpoch = epoch
	}
}

func withMarriageCertificate(sig *signing.EdSigner, refAtx types.ATXID, atxPublisher types.NodeID) testAtxV2Opt {
	return func(atx *ActivationTxV2) {
		certificate := MarriageCertificate{
			ReferenceAtx: refAtx,
			Signature:    sig.Sign(signing.MARRIAGE, atxPublisher.Bytes()),
		}
		atx.Marriages = append(atx.Marriages, certificate)
	}
}

func newActivationTxV2(opts ...testAtxV2Opt) *ActivationTxV2 {
	atx := &ActivationTxV2{
		PublishEpoch:   rand.N(types.EpochID(255)),
		PositioningATX: types.RandomATXID(),
		PreviousATXs:   make([]types.ATXID, 1+rand.IntN(255)),
		NiPosts: []NiPostsV2{
			{
				Membership: MerkleProofV2{
					Nodes: make([]types.Hash32, 32),
				},
				Challenge: types.RandomHash(),
				Posts: []SubPostV2{
					{
						MarriageIndex: rand.Uint32N(256),
						PrevATXIndex:  0,
						Post: PostV1{
							Nonce:   0,
							Indices: make([]byte, 800),
							Pow:     0,
						},
					},
				},
			},
		},
	}
	for _, opt := range opts {
		opt(atx)
	}
	return atx
}

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
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		atx := &ActivationTxV2{
			PublishEpoch:   0,
			PositioningATX: types.RandomATXID(),
			PreviousATXs:   make([]types.ATXID, 256),
			NiPosts: []NiPostsV2{
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
			},
		}
		for i := range atx.NiPosts[0].Posts {
			atx.NiPosts[0].Posts[i].Post = PostV1{
				Nonce:   0,
				Indices: make([]byte, 800),
				Pow:     0,
			}
		}
		for i := range atx.NiPosts[1].Posts {
			atx.NiPosts[1].Posts[i].Post = PostV1{
				Nonce:   0,
				Indices: make([]byte, 800),
				Pow:     0,
			}
		}
		atx.MarriageATX = new(types.ATXID)
		b.StartTimer()
		atx.ID()
	}
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
