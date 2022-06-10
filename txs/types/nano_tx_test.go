package types

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	ctypes "github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/sdk"
	"github.com/spacemeshos/go-spacemesh/vm/sdk/wallet"
)

func createMeshTX(t *testing.T, signer *signing.EdSigner, lid ctypes.LayerID) *ctypes.MeshTransaction {
	t.Helper()
	nonce := ctypes.Nonce{Counter: 223}
	amount := uint64(rand.Int())
	tx := wallet.Spend(signer.PrivateKey(), ctypes.Address{1, 2, 3}, amount, sdk.WithNonce(nonce))
	parsed := ctypes.ParsedTx{
		RawTx: ctypes.NewRawTx(tx),
	}
	parsed.MaxGas = 32132
	parsed.GasPrice = 1
	parsed.MaxSpend = amount
	parsed.Nonce = nonce
	parsed.Principal = types.BytesToAddress(signer.PublicKey().Bytes())
	return &ctypes.MeshTransaction{
		ParsedTx: parsed,
		LayerID:  lid,
		BlockID:  ctypes.BlockID{1, 3, 5},
		Received: time.Now(),
		State:    ctypes.MEMPOOL,
	}
}

func TestNewNanoTX(t *testing.T) {
	mtx := createMeshTX(t, signing.NewEdSigner(), ctypes.NewLayerID(13))
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
	mtx := createMeshTX(t, signing.NewEdSigner(), ctypes.LayerID{})
	ntx := NewNanoTX(mtx)
	lid := ctypes.NewLayerID(23)
	bid := ctypes.RandomBlockID()
	require.NotEqual(t, lid, ntx.Layer)
	require.NotEqual(t, bid, ntx.Block)
	ntx.UpdateLayerMaybe(lid, bid)
	require.Equal(t, lid, ntx.Layer)
	require.Equal(t, bid, ntx.Block)

	lid = lid.Sub(1)
	ntx.UpdateLayerMaybe(lid, ctypes.EmptyBlockID)
	require.Equal(t, lid, ntx.Layer)
	require.Equal(t, ctypes.EmptyBlockID, ntx.Block)

	lid = lid.Add(1)
	bid = ctypes.RandomBlockID()
	ntx.UpdateLayerMaybe(lid, ctypes.RandomBlockID())
	require.Equal(t, lid.Sub(1), ntx.Layer)
	require.Equal(t, ctypes.EmptyBlockID, ntx.Block)
}

func TestBetter_PanicOnInvalidArguments(t *testing.T) {
	signer := signing.NewEdSigner()
	ntx0 := NewNanoTX(createMeshTX(t, signer, ctypes.LayerID{}))
	ntx1 := NewNanoTX(createMeshTX(t, signing.NewEdSigner(), ctypes.LayerID{}))
	require.Panics(t, func() { ntx0.Better(ntx1, nil) })

	ntx2 := NewNanoTX(createMeshTX(t, signer, ctypes.LayerID{}))
	ntx2.Nonce.Counter = ntx0.Nonce.Counter + 1
	require.Panics(t, func() { ntx0.Better(ntx2, nil) })
}

func TestBetter(t *testing.T) {
	signer := signing.NewEdSigner()
	ntx0 := NewNanoTX(createMeshTX(t, signer, ctypes.LayerID{}))
	ntx1 := NewNanoTX(createMeshTX(t, signer, ctypes.LayerID{}))
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
