package wire

import (
	"math/rand/v2"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type testAtxV2Opt func(*ActivationTxV2)

func WithMarriageCertificate(sig *signing.EdSigner, refAtx types.ATXID, atxPublisher types.NodeID) testAtxV2Opt {
	return func(atx *ActivationTxV2) {
		certificate := MarriageCertificate{
			ReferenceAtx: refAtx,
			Signature:    sig.Sign(signing.MARRIAGE, atxPublisher.Bytes()),
		}
		atx.Marriages = append(atx.Marriages, certificate)
	}
}

func WithMarriageATX(id types.ATXID) testAtxV2Opt {
	return func(atx *ActivationTxV2) {
		atx.MarriageATX = &id
	}
}

func WithPublishEpoch(epoch types.EpochID) testAtxV2Opt {
	return func(atx *ActivationTxV2) {
		atx.PublishEpoch = epoch
	}
}

func WithInitial(commitAtx types.ATXID, post PostV1) testAtxV2Opt {
	return func(atx *ActivationTxV2) {
		atx.Initial = &InitialAtxPartsV2{
			CommitmentATX: commitAtx,
			Post:          post,
		}
	}
}

func WithPreviousATXs(atxs ...types.ATXID) testAtxV2Opt {
	return func(atx *ActivationTxV2) {
		atx.PreviousATXs = atxs
	}
}

func WithNIPost(opts ...testNIPostV2Opt) testAtxV2Opt {
	return func(atx *ActivationTxV2) {
		nipost := &NIPostV2{}
		for _, opt := range opts {
			opt(nipost)
		}
		atx.NIPosts = append(atx.NIPosts, *nipost)
	}
}

type testNIPostV2Opt func(*NIPostV2)

func WithNIPostChallenge(challenge types.Hash32) testNIPostV2Opt {
	return func(nipost *NIPostV2) {
		nipost.Challenge = challenge
	}
}

func WithNIPostMembershipProof(proof MerkleProofV2) testNIPostV2Opt {
	return func(nipost *NIPostV2) {
		nipost.Membership = proof
	}
}

func WithNIPostSubPost(subPost SubPostV2) testNIPostV2Opt {
	return func(nipost *NIPostV2) {
		nipost.Posts = append(nipost.Posts, subPost)
	}
}

// NewTestActivationTxV2 creates a new ActivationTxV2 with random values.
func NewTestActivationTxV2(tb testing.TB, opts ...testAtxV2Opt) *ActivationTxV2 {
	tb.Helper()
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
