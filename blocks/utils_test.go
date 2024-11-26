package blocks

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(5)
	os.Exit(m.Run())
}

func checkInNonceOrder(tb testing.TB, ids []types.TransactionID, byId map[types.TransactionID]*types.MeshTransaction) {
	accounts := make(map[types.Address]uint64)
	for _, tid := range ids {
		mtx, ok := byId[tid]
		require.True(tb, ok)
		if nonce, ok := accounts[mtx.Principal]; ok {
			require.Greater(tb, mtx.Nonce, nonce)
		}
		accounts[mtx.Principal] = mtx.Nonce
	}
}

func Test_getBlockTXs(t *testing.T) {
	numAccounts := 100
	maxNumTxs := 3
	accounts := make([]types.Address, 0, numAccounts)
	mtxs := make([]*types.MeshTransaction, 0, maxNumTxs*len(accounts))
	byTid := make(map[types.TransactionID]*types.MeshTransaction)
	for i := 0; i < numAccounts; i++ {
		numTxs := rand.IntN(maxNumTxs)
		if numTxs == 0 {
			numTxs = i + 1
		}
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		principal := types.GenerateAddress(signer.PublicKey().Bytes())
		accounts = append(accounts, principal)
		nextNonce := rand.Uint64()
		for j := 0; j < numTxs; j++ {
			tx := genTx(t, signer, types.Address{1, 2, 3, 4}, 1000, nextNonce, 10)
			mtx := &types.MeshTransaction{Transaction: tx}
			mtxs = append(mtxs, mtx)
			byTid[mtx.ID] = mtx
			nextNonce = nextNonce + 1 + rand.Uint64()
		}
	}
	lg := zaptest.NewLogger(t)

	blockSeed := types.RandomHash().Bytes()
	// no pruning
	got, err := getBlockTXs(lg, mtxs, blockSeed, 0)
	require.NoError(t, err)
	require.Len(t, got, len(mtxs))
	checkInNonceOrder(t, got, byTid)

	// make sure order is stable
	got2, err := getBlockTXs(lg, mtxs, blockSeed, 0)
	require.NoError(t, err)
	require.Equal(t, got, got2)

	// pruning
	expSize := len(mtxs) / 2
	gasLimit := uint64(expSize) * defaultGas
	got, err = getBlockTXs(lg, mtxs, blockSeed, gasLimit)
	require.NoError(t, err)
	require.Len(t, got, expSize)
	checkInNonceOrder(t, got, byTid)

	// make sure order is stable
	got2, err = getBlockTXs(lg, mtxs, blockSeed, gasLimit)
	require.NoError(t, err)
	require.Equal(t, got, got2)

	// all txs are applied
	for _, mtx := range mtxs {
		mtx.LayerID = types.LayerID(11)
	}
	got, err = getBlockTXs(lg, mtxs, blockSeed, 0)
	require.NoError(t, err)
	require.Empty(t, got)

	// empty block
	got, err = getBlockTXs(lg, nil, blockSeed, 0)
	require.NoError(t, err)
	require.Empty(t, got)
}

func Test_getBlockTXs_expected_order(t *testing.T) {
	numAccounts := uint64(10)
	accounts := make([]types.Address, 0, numAccounts)
	mtxs := make([]*types.MeshTransaction, 0, len(accounts))

	txIds := []string{ // the TXs in the order they are generated
		"411dd51b96e05c4695843b69402c983e64a66347defcac84971977a57be9b3ae",
		"173237793669ebd2ce24dea6d6e98ec07d82d8c4c96e3fa78cae397f57e15085",
		"58869e3456c3ea6ea6f9ae9ff556d0eb5d6cdbeb1d32fabbf1f5cf4051c91ab6",
		"e74c807c99bbfa60546a4f2dda96bf3445d3bbdec2adcfcd17a1f6ced0005c6e",
		"ad6c650b7aa944617ce535886183f13c95c391204020ed588fc329fc1b2fa211",
		"531613598d8339c6238e34680425c900d1ed2e4a8705a41e9ee1a1ae9518fa48",
		"1db4d0f43efd0f2f247caa3102b315d79844bb3f17dbef6381bcf30515374c46",
		"1da5cf24ef4bddf3c99150b035bbe931fd1bea54cbd16db86c8999dc0b1e9ed3",
		"0d82c6ad23f221478f4de62e249d4b4c5e0b8ec7065f775e27a299bcba2cc972",
		"b637391e3859147dbe11fbfee60c473354b08da21706df7a6c0c0262d559106c",
	}
	expectedOrder := []string{ // the TXs as they are expected to be ordered in a block
		"0d82c6ad23f221478f4de62e249d4b4c5e0b8ec7065f775e27a299bcba2cc972",
		"1da5cf24ef4bddf3c99150b035bbe931fd1bea54cbd16db86c8999dc0b1e9ed3",
		"b637391e3859147dbe11fbfee60c473354b08da21706df7a6c0c0262d559106c",
		"173237793669ebd2ce24dea6d6e98ec07d82d8c4c96e3fa78cae397f57e15085",
		"531613598d8339c6238e34680425c900d1ed2e4a8705a41e9ee1a1ae9518fa48",
		"58869e3456c3ea6ea6f9ae9ff556d0eb5d6cdbeb1d32fabbf1f5cf4051c91ab6",
		"ad6c650b7aa944617ce535886183f13c95c391204020ed588fc329fc1b2fa211",
		"411dd51b96e05c4695843b69402c983e64a66347defcac84971977a57be9b3ae",
		"1db4d0f43efd0f2f247caa3102b315d79844bb3f17dbef6381bcf30515374c46",
		"e74c807c99bbfa60546a4f2dda96bf3445d3bbdec2adcfcd17a1f6ced0005c6e",
	}

	for i := uint64(0); i < numAccounts; i++ {
		seed := fmt.Sprintf("private key %20d", i)
		key := ed25519.NewKeyFromSeed([]byte(seed))
		signer, err := signing.NewEdSigner(signing.WithPrivateKey(key))
		require.NoError(t, err)

		principal := types.GenerateAddress(signer.PublicKey().Bytes())
		accounts = append(accounts, principal)

		tx := genTx(t, signer, types.Address{1, 2, 3, 4}, 1000, i, 10)
		require.Equal(t, txIds[i], tx.ID.String(), "unexpected tx id: %d", i)
		mtx := &types.MeshTransaction{Transaction: tx}
		mtxs = append(mtxs, mtx)
	}

	blockSeed := fmt.Sprintf("block seed %21d", 101)
	got, err := getBlockTXs(zaptest.NewLogger(t), mtxs, []byte(blockSeed), 0)
	require.NoError(t, err)

	require.Len(t, got, len(mtxs))

	for i := range got {
		require.Equal(t, expectedOrder[i], got[i].String(), "unexpected tx order: %d", i)
	}
}

func Test_getProposalMetadata(t *testing.T) {
	lg := zaptest.NewLogger(t)
	db := statesql.InMemoryTest(t)
	data := atxsdata.New()
	cfg := Config{OptFilterThreshold: 70}
	lid := types.LayerID(111)
	_, atxs := createATXs(t, data, (lid.GetEpoch() - 1).FirstLayer(), 10)
	actives := types.ATXIDList(types.ToATXIDs(atxs))
	props := make([]*types.Proposal, 0, 10)
	hash1 := types.Hash32{1, 2, 3}
	hash2 := types.Hash32{3, 2, 1}
	for i := 0; i < 10; i++ {
		var p types.Proposal
		p.Layer = lid
		p.AtxID = atxs[i].ID()
		if i < 5 {
			p.MeshHash = hash1
		} else {
			p.MeshHash = hash2
		}
		for j := 0; j <= i; j++ {
			p.EligibilityProofs = append(p.EligibilityProofs, types.VotingEligibility{J: uint32(j + 1)})
		}
		p.EpochData = &types.EpochData{ActiveSetHash: actives.Hash()}
		p.EpochData.EligibilityCount = uint32(i + 1)
		props = append(props, &p)
	}
	require.NoError(t, activesets.Add(db, actives.Hash(), &types.EpochActiveSet{
		Epoch: lid.GetEpoch(),
		Set:   actives,
	}))
	require.NoError(t, layers.SetMeshHash(db, lid-1, hash2))

	// only 5 / 10 proposals has the same state
	// eligibility wise 40 / 55 has the same state
	md, err := getProposalMetadata(context.Background(), lg, db, data, cfg, lid, props)
	require.NoError(t, err)
	require.True(t, md.optFilter)
}
