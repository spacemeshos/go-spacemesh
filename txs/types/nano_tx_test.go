package types

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ctypes "github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/transaction"
)

func createMeshTX(t *testing.T, signer *signing.EdSigner, lid ctypes.LayerID) *ctypes.MeshTransaction {
	t.Helper()
	tx, err := transaction.GenerateCallTransaction(signer, ctypes.Address{1, 2, 3}, 223, uint64(rand.Intn(10000)), 31, 179997)
	require.NoError(t, err)
	return &ctypes.MeshTransaction{
		Transaction: *tx,
		LayerID:     lid,
		BlockID:     ctypes.BlockID{1, 3, 5},
		Received:    time.Now(),
		State:       ctypes.MEMPOOL,
	}
}

func TestNewNanoTX(t *testing.T) {
	mtx := createMeshTX(t, signing.NewEdSigner(), ctypes.NewLayerID(13))
	ntx := NewNanoTX(mtx)
	require.Equal(t, mtx.ID(), ntx.Tid)
	require.Equal(t, mtx.Origin(), ntx.Principal)
	require.Equal(t, mtx.Fee, ntx.Fee)
	require.Equal(t, mtx.MaxGas(), ntx.MaxGas)
	require.Equal(t, mtx.Received, ntx.Received)
	require.Equal(t, mtx.Amount, ntx.Amount)
	require.Equal(t, mtx.AccountNonce, ntx.Nonce)
	require.Equal(t, mtx.BlockID, ntx.Block)
	require.Equal(t, mtx.LayerID, ntx.Layer)
	require.Equal(t, mtx.Amount+mtx.Fee, ntx.MaxSpending())
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
	ntx2.Nonce = ntx0.Nonce + 1
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
	ntx1.Fee = ntx0.Fee + 1
	require.False(t, ntx0.Better(ntx1, nil))

	ntx1.Fee = ntx0.Fee
	require.NotEqual(t, ntx0.Tid, ntx1.Tid)
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
