package fixture

import (
	"math/rand"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
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

// Next generates ActivationTx.
func (g *AtxsGenerator) Next() *types.ActivationTx {
	var nodeID types.NodeID
	g.rng.Read(nodeID[:])

	atx := &types.ActivationTx{
		Sequence:     g.rng.Uint64(),
		PublishEpoch: g.Epochs[g.rng.Intn(len(g.Epochs))],
		Coinbase:     wallet.Address(nodeID.Bytes()),
		NumUnits:     g.rng.Uint32(),
		TickCount:    1,
		SmesherID:    nodeID,
	}
	atx.SetID(types.RandomATXID())
	atx.SetReceived(time.Now().Local())
	return atx
}
