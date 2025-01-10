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
		"7f3542568634764a1880c3c26898d02a8cef7ee0cbdbe7136cdd3085031454b1",
		"2f3bb693cec084cd5cedf3dea876ebfa8e9be8b37c1e15b6edc282194c14b1e6",
		"82a1bf4970c7044ffb17bd86f8b334573118c16855ace7865d9cf30bbda2bb8b",
		"3c98b5bf80f7fa0ba3cc8306e3652a77c98e1d110ad9620777c1b185ddb6e460",
		"53ac61fbb47158716a33d3c8857fd550922bf651c3e70a6343e32f8b8bf9303a",
		"22d6efe013ba825d76079ae13fe5c23c6dbaefdf4963d66b878fdce8aa57b7f1",
		"f91bea440368fbb50a4db30ee4c501662af99ca60777036445dc41f66b7c733a",
		"79f977b5ba31cd13a42a92c2790cfa6ff22b2b67669f4ec668fa031e6b517aad",
		"50dcb99991b7e096ec9f1d795091b417b0b9bd9cd911b0117259af2a4994dde2",
		"a1087106936360975db95f36f165f3919870f754a76b129984932e75e79d2c64",
	}
	expectedOrder := []string{
		"22d6efe013ba825d76079ae13fe5c23c6dbaefdf4963d66b878fdce8aa57b7f1",
		"3c98b5bf80f7fa0ba3cc8306e3652a77c98e1d110ad9620777c1b185ddb6e460",
		"a1087106936360975db95f36f165f3919870f754a76b129984932e75e79d2c64",
		"2f3bb693cec084cd5cedf3dea876ebfa8e9be8b37c1e15b6edc282194c14b1e6",
		"79f977b5ba31cd13a42a92c2790cfa6ff22b2b67669f4ec668fa031e6b517aad",
		"7f3542568634764a1880c3c26898d02a8cef7ee0cbdbe7136cdd3085031454b1",
		"82a1bf4970c7044ffb17bd86f8b334573118c16855ace7865d9cf30bbda2bb8b",
		"53ac61fbb47158716a33d3c8857fd550922bf651c3e70a6343e32f8b8bf9303a",
		"50dcb99991b7e096ec9f1d795091b417b0b9bd9cd911b0117259af2a4994dde2",
		"f91bea440368fbb50a4db30ee4c501662af99ca60777036445dc41f66b7c733a",
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
