package txs

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type MeshTransactionOption func(*types.MeshTransaction)

func WithReceived(received time.Time) MeshTransactionOption {
	return func(h *types.MeshTransaction) {
		h.Received = received
	}
}

func createMeshTX(
	t *testing.T,
	signer *signing.EdSigner,
	lid types.LayerID,
	opts ...MeshTransactionOption,
) *types.MeshTransaction {
	t.Helper()
	nonce := uint64(223)
	amount := uint64(rand.Int())
	tx := wallet.Spend(signer.PrivateKey(), types.Address{1, 2, 3}, amount, nonce)
	parsed := types.Transaction{
		RawTx:    types.NewRawTx(tx),
		TxHeader: &types.TxHeader{},
	}
	parsed.MaxGas = 32132
	parsed.GasPrice = 1
	parsed.MaxSpend = amount
	parsed.Nonce = nonce
	parsed.Principal = types.GenerateAddress(signer.PublicKey().Bytes())
	meshTx := types.MeshTransaction{
		Transaction: parsed,
		LayerID:     lid,
		BlockID:     types.BlockID{1, 3, 5},
		Received:    time.Now(),
		State:       types.MEMPOOL,
	}

	for _, opt := range opts {
		opt(&meshTx)
	}

	return &meshTx
}

func TestNewNanoTX(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	mtx := createMeshTX(t, sig, types.LayerID(13))
	ntx := NewNanoTX(mtx)
	require.Equal(t, mtx.ID, ntx.ID)
	require.Equal(t, mtx.Principal, ntx.Principal)
	require.Equal(t, mtx.Fee(), ntx.Fee())
	require.Equal(t, mtx.MaxGas, ntx.MaxGas)
	require.Equal(t, mtx.Received, ntx.Received)
	require.Equal(t, mtx.MaxSpend, ntx.MaxSpend)
	require.Equal(t, mtx.Nonce, ntx.Nonce)
	require.Equal(t, mtx.BlockID, ntx.Block)
	require.Equal(t, mtx.LayerID, ntx.Layer)
	require.Equal(t, mtx.MaxSpend+mtx.Fee(), ntx.MaxSpending())
}

func TestUpdateMaybe(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	mtx := createMeshTX(t, sig, 0)
	ntx := NewNanoTX(mtx)
	lid := types.LayerID(23)
	bid := types.RandomBlockID()
	require.NotEqual(t, lid, ntx.Layer)
	require.NotEqual(t, bid, ntx.Block)
	ntx.UpdateLayerMaybe(lid, bid)
	require.Equal(t, lid, ntx.Layer)
	require.Equal(t, bid, ntx.Block)

	lid = lid.Sub(1)
	ntx.UpdateLayerMaybe(lid, types.EmptyBlockID)
	require.Equal(t, lid, ntx.Layer)
	require.Equal(t, types.EmptyBlockID, ntx.Block)

	lid = lid.Add(1)
	ntx.UpdateLayerMaybe(lid, types.RandomBlockID())
	require.Equal(t, lid.Sub(1), ntx.Layer)
	require.Equal(t, types.EmptyBlockID, ntx.Block)
}

func TestBetter_PanicOnInvalidArguments(t *testing.T) {
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	received := time.Now()
	ntx0 := NewNanoTX(createMeshTX(t, signer1, 0, WithReceived(received)))
	received = received.Add(time.Nanosecond)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)
	ntx1 := NewNanoTX(createMeshTX(t, signer2, 0, WithReceived(received)))
	require.Panics(t, func() { ntx0.Better(ntx1, nil) })

	received = received.Add(time.Nanosecond)
	ntx2 := NewNanoTX(createMeshTX(t, signer1, 0, WithReceived(received)))
	ntx2.Nonce = ntx0.Nonce + 1
	require.Panics(t, func() { ntx0.Better(ntx2, nil) })
}

func TestBetter(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	received := time.Now()
	ntx0 := NewNanoTX(createMeshTX(t, signer, 0, WithReceived(received)))
	ntx1 := NewNanoTX(createMeshTX(t, signer, 0, WithReceived(received.Add(time.Nanosecond))))
	require.Equal(t, ntx0.Principal, ntx1.Principal)
	require.Equal(t, ntx0.Nonce, ntx1.Nonce)
	// fees are equal, ntx0 is better due to earlier timestamp
	require.True(t, ntx0.Better(ntx1, nil))

	// ntx1 now has higher fee, so it's better than ntx0
	ntx1.GasPrice = ntx0.GasPrice + 1
	require.False(t, ntx0.Better(ntx1, nil))

	ntx1.GasPrice = ntx0.GasPrice
	require.NotEqual(t, ntx0.ID, ntx1.ID)
	// transaction selection for block needs to be stable. test that
	// the comparison the same
	blockSeed := []byte{3, 2, 1}
	better := ntx0.Better(ntx1, blockSeed)
	// test that it's stable
	repeat := 1000
	for i := 0; i < repeat; i++ {
		require.Equal(t, better, ntx0.Better(ntx1, blockSeed))
	}
}
