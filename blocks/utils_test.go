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

func checkInNonceOrder(t *testing.T, tids []types.TransactionID, byTid map[types.TransactionID]*types.MeshTransaction) {
	accounts := make(map[types.Address]uint64)
	for _, tid := range tids {
		mtx, ok := byTid[tid]
		require.True(t, ok)
		if nonce, ok := accounts[mtx.Principal]; ok {
			require.Greater(t, mtx.Nonce, nonce)
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
		"9e8963dbd7566c06e007f0dab910168aab24d1ae9789cb896d2fb24778fc6002",
		"9a96ccb0ca208ac91e0a0a92f7c6208f83a13a649582b695a56f9c981e39b619",
		"98f095630e631fd0057304443ebd24798d58d95fa5394f9eecbf75d9a4fa6e29",
		"8ec40d3c67bb73595f61912348a48d038a60aeead84ecb1d5e5d2476071a8b73",
		"83f3bb7d51c874fbf2541f69e52e09b810364cf329e55b0f328d072ca465efa9",
		"c3c9b8af8a3d0966dfeeef69d15202e17898d9c2f26e6783c948e59428231252",
		"08ea037ab3400a575f7d7911a8c7cdc2175c008ea7a98ee24f14611ee5e5339f",
		"8058885d69129db2757b3d5b3fb214d9aaf5640474486ae71cb8a79752474d7b",
		"db6d9aad9dafcb11d1da1c9156caeb5329f6430f6df2b0287b0afb08e0f7c4c4",
		"1c8e203412e9eb00173870dc62f25b9c142ea0c64d7831ac236aab8c94a76865",
	}
	expectedOrder := []string{ // the TXs as they are expected ot be ordered in a block
		"08ea037ab3400a575f7d7911a8c7cdc2175c008ea7a98ee24f14611ee5e5339f",
		"8058885d69129db2757b3d5b3fb214d9aaf5640474486ae71cb8a79752474d7b",
		"c3c9b8af8a3d0966dfeeef69d15202e17898d9c2f26e6783c948e59428231252",
		"1c8e203412e9eb00173870dc62f25b9c142ea0c64d7831ac236aab8c94a76865",
		"98f095630e631fd0057304443ebd24798d58d95fa5394f9eecbf75d9a4fa6e29",
		"9a96ccb0ca208ac91e0a0a92f7c6208f83a13a649582b695a56f9c981e39b619",
		"9e8963dbd7566c06e007f0dab910168aab24d1ae9789cb896d2fb24778fc6002",
		"8ec40d3c67bb73595f61912348a48d038a60aeead84ecb1d5e5d2476071a8b73",
		"83f3bb7d51c874fbf2541f69e52e09b810364cf329e55b0f328d072ca465efa9",
		"db6d9aad9dafcb11d1da1c9156caeb5329f6430f6df2b0287b0afb08e0f7c4c4",
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
	db := statesql.InMemory()
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
