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
		"c98e047d529453e7287421789606b8c446a6503e079b190a0f5c7f3b0f5cb612",
		"4c2a648306b176a7fbbff15ad802887dc128aee767af4c9de98286e5ad2566ac",
		"e3d5feed299c4e392a6c18293eb411435eab53eee0b9011b85446bc38319e45f",
		"25c581428075004f20ed5d4042e5386cd788dbda9552c518ccc048d48cddbfbd",
		"98e23bb58e6bcf1b8905e3d6ab2338732f750d1d87af77730f08c809b933d090",
		"57c9bfaf38518d77007f19a06c38ce87ea31ba2fd2479b0b7d0676b352dc58a2",
		"75cc92666ea4d5e91c15b4c14bfd1d62b2dbb08f19c7af1ff4120c647bd1bc32",
		"4b3368c2be18f0699f1efbf40229e72a33d82af053a6ab7619cb427bee79d6fa",
		"84750a6878516e9b377b8916c593b1e18cd5f20488acc07d803e9c593557c296",
		"ca331e2383e6d6b2f50114037422b7ef8f4aa67954bd80c7d0ce50f38f0ba970",
	}
	expectedOrder := []string{
		"25c581428075004f20ed5d4042e5386cd788dbda9552c518ccc048d48cddbfbd",
		"4c2a648306b176a7fbbff15ad802887dc128aee767af4c9de98286e5ad2566ac",
		"ca331e2383e6d6b2f50114037422b7ef8f4aa67954bd80c7d0ce50f38f0ba970",
		"4b3368c2be18f0699f1efbf40229e72a33d82af053a6ab7619cb427bee79d6fa",
		"84750a6878516e9b377b8916c593b1e18cd5f20488acc07d803e9c593557c296",
		"98e23bb58e6bcf1b8905e3d6ab2338732f750d1d87af77730f08c809b933d090",
		"c98e047d529453e7287421789606b8c446a6503e079b190a0f5c7f3b0f5cb612",
		"75cc92666ea4d5e91c15b4c14bfd1d62b2dbb08f19c7af1ff4120c647bd1bc32",
		"57c9bfaf38518d77007f19a06c38ce87ea31ba2fd2479b0b7d0676b352dc58a2",
		"e3d5feed299c4e392a6c18293eb411435eab53eee0b9011b85446bc38319e45f",
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
