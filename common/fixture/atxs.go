package fixture

import (
	"math/rand"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
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

// Next generates VerifiedActivationTx.
func (g *AtxsGenerator) Next() *wire.ActivationTxV1 {
	var prevAtxId types.ATXID
	g.rng.Read(prevAtxId[:])
	var posAtxId types.ATXID
	g.rng.Read(posAtxId[:])

	signer, err := signing.NewEdSigner(signing.WithKeyFromRand(g.rng))
	if err != nil {
		panic("failed to create signer")
	}

	atx := &wire.ActivationTxV1{
		InnerActivationTxV1: wire.InnerActivationTxV1{
			NIPostChallengeV1: wire.NIPostChallengeV1{
				Sequence:         g.rng.Uint64(),
				PrevATXID:        prevAtxId,
				PublishEpoch:     g.Epochs[g.rng.Intn(len(g.Epochs))],
				PositioningATXID: posAtxId,
			},
			Coinbase: wallet.Address(signer.PublicKey().Bytes()),
			NumUnits: g.rng.Uint32(),
		},
	}
	atx.Sign(signer)
	return atx
}

func ToAtx(t testing.TB, watx *wire.ActivationTxV1) *types.ActivationTx {
	t.Helper()
	atx := wire.ActivationTxFromWireV1(watx)
	atx.SetReceived(time.Now().Local())
	atx.TickCount = 1
	return atx
}
