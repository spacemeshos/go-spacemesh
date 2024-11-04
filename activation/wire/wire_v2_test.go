package wire

import (
	"fmt"
	"math/rand/v2"
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type testAtxV2Opt func(*ActivationTxV2)

func withMarriageCertificate(sig *signing.EdSigner, refAtx types.ATXID, atxPublisher types.NodeID) testAtxV2Opt {
	return func(atx *ActivationTxV2) {
		certificate := MarriageCertificate{
			ReferenceAtx: refAtx,
			Signature:    sig.Sign(signing.MARRIAGE, atxPublisher.Bytes()),
		}
		atx.Marriages = append(atx.Marriages, certificate)
	}
}

func withMarriageATX(id types.ATXID) testAtxV2Opt {
	return func(atx *ActivationTxV2) {
		atx.MarriageATX = &id
	}
}

func withPublishEpoch(epoch types.EpochID) testAtxV2Opt {
	return func(atx *ActivationTxV2) {
		atx.PublishEpoch = epoch
	}
}

func withInitial(commitAtx types.ATXID, post PostV1) testAtxV2Opt {
	return func(atx *ActivationTxV2) {
		atx.Initial = &InitialAtxPartsV2{
			CommitmentATX: commitAtx,
			Post:          post,
		}
	}
}

func withPreviousATXs(atxs ...types.ATXID) testAtxV2Opt {
	return func(atx *ActivationTxV2) {
		atx.PreviousATXs = atxs
	}
}

func withNIPost(opts ...testNIPostV2Opt) testAtxV2Opt {
	return func(atx *ActivationTxV2) {
		nipost := &NIPostV2{}
		for _, opt := range opts {
			opt(nipost)
		}
		atx.NIPosts = append(atx.NIPosts, *nipost)
	}
}

type testNIPostV2Opt func(*NIPostV2)

func withNIPostChallenge(challenge types.Hash32) testNIPostV2Opt {
	return func(nipost *NIPostV2) {
		nipost.Challenge = challenge
	}
}

func withNIPostMembershipProof(proof MerkleProofV2) testNIPostV2Opt {
	return func(nipost *NIPostV2) {
		nipost.Membership = proof
	}
}

func withNIPostSubPost(subPost SubPostV2) testNIPostV2Opt {
	return func(nipost *NIPostV2) {
		nipost.Posts = append(nipost.Posts, subPost)
	}
}

func newActivationTxV2(opts ...testAtxV2Opt) *ActivationTxV2 {
	atx := &ActivationTxV2{
		PublishEpoch:   rand.N(types.EpochID(255)),
		PositioningATX: types.RandomATXID(),
	}
	for _, opt := range opts {
		opt(atx)
	}
	if atx.PreviousATXs == nil {
		atx.PreviousATXs = make([]types.ATXID, 1+rand.IntN(255))
	}
	if atx.NIPosts == nil {
		atx.NIPosts = []NIPostV2{
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
		}
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
			},
		}
		for i := range atx.NIPosts[0].Posts {
			atx.NIPosts[0].Posts[i].Post = PostV1{
				Nonce:   0,
				Indices: make([]byte, 800),
				Pow:     0,
			}
		}
		for i := range atx.NIPosts[1].Posts {
			atx.NIPosts[1].Posts[i].Post = PostV1{
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
