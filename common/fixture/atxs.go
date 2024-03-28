package fixture

import (
	"math/rand"
	"time"

	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/poet/shared"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// NewAtxsGenerator with some random parameters.
func NewAtxsGenerator() *AtxsGenerator {
	return new(AtxsGenerator).
		WithSeed(time.Now().UnixNano()).
		WithEpochs(0, 10)
}

// AtxsGenerator generates random activations.
// Activations are not syntactically or contextually valid. This is for testing databases and APIs.
type AtxsGenerator struct {
	rng *rand.Rand

	Epochs []types.EpochID
	Addrs  []types.Address
}

// WithSeed update randomness source.
func (g *AtxsGenerator) WithSeed(seed int64) *AtxsGenerator {
	g.rng = rand.New(rand.NewSource(seed))
	return g
}

// WithEpochs update epochs ids.
func (g *AtxsGenerator) WithEpochs(start, n int) *AtxsGenerator {
	g.Epochs = nil
	for i := 0; i < n; i++ {
		g.Epochs = append(g.Epochs, types.EpochID(start+i))
	}
	return g
}

func (g *AtxsGenerator) newNIPost() *types.NIPost {
	challenge := types.HexToHash32("55555")
	poetRef := []byte("66666")
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(shared.HashMembershipTreeNode).
		WithLeavesToProve(map[uint64]bool{0: true}).
		Build()
	if err != nil {
		panic("failed to add leaf to tree")
	}
	if err := tree.AddLeaf(challenge[:]); err != nil {
		panic("failed to add leaf to tree")
	}
	nodes := tree.Proof()
	nodesH32 := make([]types.Hash32, 0, len(nodes))
	for _, n := range nodes {
		nodesH32 = append(nodesH32, types.BytesToHash(n))
	}
	return &types.NIPost{
		Membership: types.MerkleProof{
			Nodes: nodesH32,
		},
		Post: types.Post{
			Nonce:   0,
			Indices: []byte(nil),
		},
		PostMetadata: types.PostMetadata{
			Challenge:     poetRef,
			LabelsPerUnit: 2048,
		},
	}
}

// Next generates VerifiedActivationTx.
func (g *AtxsGenerator) Next() *types.VerifiedActivationTx {
	var atx types.VerifiedActivationTx

	var prevAtxId types.ATXID
	g.rng.Read(prevAtxId[:])
	var posAtxId types.ATXID
	g.rng.Read(posAtxId[:])
	var nodeId types.NodeID
	g.rng.Read(nodeId[:])

	signer, err := signing.NewEdSigner()
	if err != nil {
		panic("failed to create signer")
	}

	atx = types.VerifiedActivationTx{
		ActivationTx: &types.ActivationTx{
			InnerActivationTx: types.InnerActivationTx{
				NIPostChallenge: types.NIPostChallenge{
					Sequence:       g.rng.Uint64(),
					PrevATXID:      prevAtxId,
					PublishEpoch:   g.Epochs[g.rng.Intn(len(g.Epochs))],
					PositioningATX: posAtxId,
				},
				Coinbase: wallet.Address(signer.PublicKey().Bytes()),
				NumUnits: g.rng.Uint32(),
				NodeID:   &nodeId,
				NIPost:   *g.newNIPost(),
			},
			SmesherID: nodeId,
		},
	}

	atx.SetEffectiveNumUnits(atx.NumUnits)

	var atxId types.ATXID
	g.rng.Read(atxId[:])
	atx.SetID(atxId)

	return &atx
}
