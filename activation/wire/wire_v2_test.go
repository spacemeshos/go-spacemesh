package wire

import (
	"encoding/binary"
	"math/rand/v2"
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/spacemeshos/merkle-tree"
	"github.com/stretchr/testify/require"

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

const PublishEpochIndex = 0

func Test_GenerateDoublePublishProof(t *testing.T) {
	atx := &ActivationTxV2{
		PublishEpoch:   10,
		PositioningATX: types.RandomATXID(),
		PreviousATXs:   make([]types.ATXID, 1),
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

	proof, err := generatePublishEpochProof(atx)
	require.NoError(t, err)
	require.NotNil(t, proof)

	// a malfeasance proof for double publish will contain
	// - the value of the PublishEpoch (here 10) - 4 bytes
	// - the two ATX IDs - 32 bytes each
	// - the two signatures (atx.Signature + atx.NodeID) - 64 bytes each
	// - two merkle proofs - one per ATX - that is 128 bytes each (4 * 32)
	// total: 452 bytes instead of two full ATXs (> 20 kB each in the worst case)

	publishEpoch := make([]byte, 4)
	binary.LittleEndian.PutUint32(publishEpoch, atx.PublishEpoch.Uint32())
	ok, err := merkle.ValidatePartialTree(
		[]uint64{PublishEpochIndex},
		[][]byte{publishEpoch},
		proof,
		atx.ID().Bytes(),
		atxTreeHash,
	)
	require.NoError(t, err)
	require.True(t, ok)

	// different PublishEpoch doesn't validate
	publishEpoch = []byte{0xFF, 0x00, 0x00, 0x00}
	ok, err = merkle.ValidatePartialTree(
		[]uint64{PublishEpochIndex},
		[][]byte{publishEpoch},
		proof,
		atx.ID().Bytes(),
		atxTreeHash,
	)
	require.NoError(t, err)
	require.False(t, ok)
}

func generatePublishEpochProof(atx *ActivationTxV2) ([][]byte, error) {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{PublishEpochIndex: true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		return nil, err
	}
	atx.merkleTree(tree)
	return tree.Proof(), nil
}
