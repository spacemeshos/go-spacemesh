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
		"167bc8dcbb8585971a7eb26c44b7d546e637e953440ead39edf408f78eb11da1",
		"4271808f3e60bc08c33dc7039f400faf98850f8103966a71c02d1afbaea8b2fb",
		"78b554a73e40d72d7827940fc2cdd113161a38728731b0193b9bde6793fee0c6",
		"be9b21e0b1d8cf7aa235cee3b715c36dfeb4aa1003448261d6788686d22b6750",
		"6eb45cf4327db077e22faad77e389edf7d80745c6ed3b7005c09f2fca0a5e2a6",
		"e1fdf283380959ed21a20644dac3afcd3d299e4ea19b9aedd61823459d8f3242",
		"aea46b8f679baa5450fe26b12c50282d58077b8a1e1bb379a4a3e13a547f57a7",
		"9ac12a41d237d6ac5655c92698949cb4cf460473e62306a69eb1a6048e799641",
		"6b71f013fcfc848bb164620927848be2f47a9801a4a1409ca9a193c390477782",
		"e12a55b9b4e8da24baad766f0d3f16ef578a5f62cefefedc5e5eaadc02ff76df",
	}
	expectedOrder := []string{
		"167bc8dcbb8585971a7eb26c44b7d546e637e953440ead39edf408f78eb11da1",
		"6b71f013fcfc848bb164620927848be2f47a9801a4a1409ca9a193c390477782",
		"e12a55b9b4e8da24baad766f0d3f16ef578a5f62cefefedc5e5eaadc02ff76df",
		"4271808f3e60bc08c33dc7039f400faf98850f8103966a71c02d1afbaea8b2fb",
		"9ac12a41d237d6ac5655c92698949cb4cf460473e62306a69eb1a6048e799641",
		"aea46b8f679baa5450fe26b12c50282d58077b8a1e1bb379a4a3e13a547f57a7",
		"be9b21e0b1d8cf7aa235cee3b715c36dfeb4aa1003448261d6788686d22b6750",
		"78b554a73e40d72d7827940fc2cdd113161a38728731b0193b9bde6793fee0c6",
		"6eb45cf4327db077e22faad77e389edf7d80745c6ed3b7005c09f2fca0a5e2a6",
		"e1fdf283380959ed21a20644dac3afcd3d299e4ea19b9aedd61823459d8f3242",
	}
	_ = txIds
	_ = expectedOrder
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
