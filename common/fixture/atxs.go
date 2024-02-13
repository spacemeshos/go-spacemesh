package fixture

import (
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/signing"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
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
	for i := 1; i <= n; i++ {
		g.Epochs = append(g.Epochs, types.EpochID(start+i))
	}
	return g
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
		log.Println("failed to create signer:", err)
		os.Exit(1)
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
			},
		},
	}

	atx.SetEffectiveNumUnits(atx.NumUnits)

	var atxId types.ATXID
	g.rng.Read(atxId[:])
	atx.SetID(atxId)

	return &atx
}
