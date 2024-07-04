package wire

import (
	"math/rand/v2"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type testAtxV2Opt func(*ActivationTxV2)

func WithPublishEpoch(epoch types.EpochID) testAtxV2Opt {
	return func(atx *ActivationTxV2) {
		atx.PublishEpoch = epoch
	}
}

func WithMarriageCertificate(sig *signing.EdSigner, refAtx types.ATXID, atxPublisher types.NodeID) testAtxV2Opt {
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
