package txs

import (
	"bytes"
	"context"
	"errors"
	"math"
	"math/rand"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/txs/mocks"
)

const (
	layersPerEpoch   = 3
	numProposals     = 10
	numTXsInProposal = 17
	numUnit          = 12
	defaultGas       = uint64(100)
	defaultBalance   = uint64(10000000)
	defaultAmount    = uint64(100)
	defaultFee       = uint64(4)
	numTXs           = 29
	nonce            = uint64(1234)
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	res := m.Run()
	os.Exit(res)
}

func newTx(t *testing.T, nonce uint64, amount, fee uint64, signer *signing.EdSigner) *types.Transaction {
	t.Helper()
	dest := types.Address{byte(rand.Int()), byte(rand.Int()), byte(rand.Int()), byte(rand.Int())}
	return newTxWthRecipient(t, dest, nonce, amount, fee, signer)
}

func newTxWthRecipient(t *testing.T, dest types.Address, nonce uint64, amount, fee uint64, signer *signing.EdSigner) *types.Transaction {
	t.Helper()
	raw := wallet.Spend(signer.PrivateKey(), dest, amount,
		types.Nonce{Counter: nonce},
		sdk.WithGasPrice(fee),
	)
	tx := types.Transaction{
		RawTx:    types.NewRawTx(raw),
		TxHeader: &types.TxHeader{},
	}
	tx.MaxGas = defaultGas
	tx.MaxSpend = amount
	tx.GasPrice = fee
	tx.Nonce = types.Nonce{Counter: nonce}
	tx.Principal = types.GenerateAddress(signer.PublicKey().Bytes())
	return &tx
}

func createATXs(t *testing.T, cdb *datastore.CachedDB, lid types.LayerID, numATXs int) ([]*signing.EdSigner, []*types.ActivationTx) {
	return createModifiedATXs(t, cdb, lid, numATXs, func(atx *types.ActivationTx) (*types.VerifiedActivationTx, error) {
		return atx.Verify(0, 1)
	})
}

func createModifiedATXs(t *testing.T, cdb *datastore.CachedDB, lid types.LayerID, numATXs int, onAtx func(*types.ActivationTx) (*types.VerifiedActivationTx, error)) ([]*signing.EdSigner, []*types.ActivationTx) {
	t.Helper()
	atxes := make([]*types.ActivationTx, 0, numATXs)
	signers := make([]*signing.EdSigner, 0, numATXs)
	for i := 0; i < numATXs; i++ {
		signer := signing.NewEdSigner()
		signers = append(signers, signer)
		address := types.GenerateAddress(signer.PublicKey().Bytes())
		atx := types.NewActivationTx(
			types.NIPostChallenge{PubLayerID: lid},
			address,
			nil,
			numUnit,
			nil,
		)
		require.NoError(t, activation.SignAtx(signer, atx))
		require.NoError(t, atx.CalcAndSetID())
		require.NoError(t, atx.CalcAndSetNodeID())
		vAtx, err := onAtx(atx)
		require.NoError(t, err)

		require.NoError(t, atxs.Add(cdb, vAtx, time.Now()))
		atxes = append(atxes, atx)
	}
	return signers, atxes
}

func createProposalsDupTXs(
	t *testing.T,
	lid types.LayerID,
	signers []*signing.EdSigner,
	activeSet []types.ATXID,
	hash types.Hash32,
	step int,
	tids []types.TransactionID,
) []*types.Proposal {
	t.Helper()
	require.Equal(t, numProposals, len(signers))
	require.Equal(t, numProposals, len(activeSet))
	epochData := &types.EpochData{
		ActiveSet: activeSet,
		Beacon:    types.RandomBeacon(),
	}
	total := len(tids)
	proposals := make([]*types.Proposal, 0, numProposals)
	from := 0
	for i := 0; i < numProposals; i++ {
		if from >= total {
			from = 0
		}
		to := from + numTXsInProposal
		if to >= total {
			to = total
		}
		p := createProposal(t, epochData, lid, activeSet[i], signers[i], tids[from:to], 1)
		p.MeshHash = hash
		proposals = append(proposals, p)
		from += step
	}
	return proposals
}

func createProposal(t *testing.T, epochData *types.EpochData, lid types.LayerID, atxID types.ATXID, signer *signing.EdSigner, txIDs []types.TransactionID, numEligibility int) *types.Proposal {
	t.Helper()
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot: types.InnerBallot{
					AtxID:             atxID,
					LayerIndex:        lid,
					EligibilityProofs: make([]types.VotingEligibilityProof, numEligibility),
					EpochData:         epochData,
				},
			},
			TxIDs: txIDs,
		},
	}
	p.Ballot.Signature = signer.Sign(p.Ballot.SignedBytes())
	p.Signature = signer.Sign(p.Bytes())
	require.NoError(t, p.Initialize())
	return p
}

func checkRewards(t *testing.T, atxs []*types.ActivationTx, expWeightPer util.Weight, rewards []types.AnyReward) {
	t.Helper()
	sort.Slice(atxs, func(i, j int) bool {
		return bytes.Compare(atxs[i].Coinbase.Bytes(), atxs[j].Coinbase.Bytes()) < 0
	})
	for i, r := range rewards {
		require.Equal(t, atxs[i].Coinbase, r.Coinbase)
		got := util.WeightFromNumDenom(r.Weight.Num, r.Weight.Denom)
		require.Equal(t, expWeightPer, got)
	}
}

type testConState struct {
	*ConservativeState
	logger log.Log
	cdb    *datastore.CachedDB
	mvm    *mocks.MockvmState
}

func (t *testConState) handler() *TxHandler {
	return NewTxHandler(t, t.logger)
}

func createTestState(t *testing.T, gasLimit uint64) *testConState {
	ctrl := gomock.NewController(t)
	mvm := mocks.NewMockvmState(ctrl)
	logger := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), logger)
	cfg := CSConfig{
		LayerSize:          10,
		LayersPerEpoch:     3,
		BlockGasLimit:      gasLimit,
		NumTXsPerProposal:  numTXsInProposal,
		OptFilterThreshold: 85,
	}
	return &testConState{
		ConservativeState: NewConservativeState(mvm, cdb,
			WithCSConfig(cfg),
			WithLogger(logger)),
		logger: logger,
		cdb:    cdb,
		mvm:    mvm,
	}
}

func createConservativeState(t *testing.T) *testConState {
	return createTestState(t, math.MaxUint64)
}

func addBatch(t *testing.T, tcs *testConState, numTXs int) ([]types.TransactionID, []types.Transaction) {
	t.Helper()
	ids := make([]types.TransactionID, 0, numTXs)
	txs := make([]types.Transaction, 0, numTXs)
	for i := 0; i < numTXs; i++ {
		signer := signing.NewEdSigner()
		addr := types.GenerateAddress(signer.PublicKey().Bytes())
		tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
		tcs.mvm.EXPECT().GetNonce(addr).Return(types.Nonce{Counter: nonce}, nil).Times(1)
		tx := newTx(t, nonce+5, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.TODO(), tx))
		ids = append(ids, tx.ID)
		txs = append(txs, *tx)
	}
	return ids, txs
}

func TestGenerateBlock_OptimisticFiltering(t *testing.T) {
	tcs := createConservativeState(t)
	totalNumTXs := 100
	tids, txs := addBatch(t, tcs, totalNumTXs)
	// for updating cache after txs are executed
	for _, tx := range txs {
		principal := tx.Principal
		tcs.mvm.EXPECT().GetBalance(principal).Return(defaultBalance, nil)
		tcs.mvm.EXPECT().GetNonce(principal).Return(types.Nonce{Counter: nonce}, nil)
	}
	// make sure proposals have overlapping transactions
	step := totalNumTXs / numProposals
	meshHash := types.RandomHash()
	lid := types.NewLayerID(1333)
	signers, atxes := createATXs(t, tcs.cdb, (lid.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	proposals := createProposalsDupTXs(t, lid, signers, activeSet, meshHash, step, tids)
	require.NoError(t, layers.SetHashes(tcs.cdb, lid.Sub(1), types.RandomHash(), meshHash))

	t.Run("out of order", func(t *testing.T) {
		require.NoError(t, layers.SetApplied(tcs.cdb, lid.Sub(2), types.RandomBlockID()))
		got, executed, err := tcs.GenerateBlock(context.Background(), lid, proposals)
		require.ErrorIs(t, err, errLayerNotInOrder)
		require.False(t, executed)
		require.Nil(t, got)
	})

	t.Run("execute in situ", func(t *testing.T) {
		require.NoError(t, layers.SetApplied(tcs.cdb, lid.Sub(1), types.RandomBlockID()))
		results := makeResults(lid, types.EmptyBlockID, txs...)
		tcs.mvm.EXPECT().Apply(vm.ApplyContext{Layer: lid}, gomock.Any(), gomock.Any()).Return(nil, results, nil)
		got, executed, err := tcs.GenerateBlock(context.Background(), lid, proposals)
		require.NoError(t, err)
		require.True(t, executed)
		require.NotEqual(t, types.EmptyBlockID, got.ID())
		require.Equal(t, lid, got.LayerIndex)
		require.Len(t, got.TxIDs, totalNumTXs)
	})
}

func TestGenerateBlock_NoOptimisticFiltering(t *testing.T) {
	tcs := createConservativeState(t)
	totalNumTXs := 100
	tids, _ := addBatch(t, tcs, totalNumTXs)
	// make sure proposals have overlapping transactions
	step := totalNumTXs / numProposals
	lid := types.NewLayerID(1333)
	signers, atxes := createATXs(t, tcs.cdb, (lid.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	meshHash := types.RandomHash()
	require.NoError(t, layers.SetHashes(tcs.cdb, lid.Sub(1), types.RandomHash(), meshHash))
	proposals := createProposalsDupTXs(t, lid, signers, activeSet, meshHash, step, tids)
	// change the first two proposals' mesh hash
	for _, p := range proposals[:2] {
		p.MeshHash = types.RandomHash()
	}
	require.NoError(t, layers.SetApplied(tcs.cdb, lid.Sub(1), types.RandomBlockID()))
	require.NoError(t, layers.SetHashes(tcs.cdb, lid.Sub(1), types.RandomHash(), meshHash))
	got, executed, err := tcs.GenerateBlock(context.Background(), lid, proposals)
	require.NoError(t, err)
	require.False(t, executed)
	require.NotEqual(t, types.EmptyBlockID, got.ID())
	require.Equal(t, lid, got.LayerIndex)
	require.Len(t, got.TxIDs, totalNumTXs)

	// reverse the order of proposals. we should still have exactly the same ordered transactions
	// reorder the proposals
	ordered := proposals[numProposals/2 : numProposals]
	ordered = append(ordered, proposals[0:numProposals/2]...)
	require.NotEqual(t, proposals, ordered)
	var got2 *types.Block
	got2, executed, err = tcs.GenerateBlock(context.Background(), lid, proposals)
	require.NoError(t, err)
	require.False(t, executed)
	require.Equal(t, got, got2)
	require.Equal(t, got.ID(), got2.ID())
}

func TestGenerateBlock_UnequalHeight(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.GetEffectiveGenesis().Add(100)
	rng := rand.New(rand.NewSource(10101))
	max := uint64(0)
	signers, atxes := createModifiedATXs(t, tcs.cdb, (lid.GetEpoch() - 1).FirstLayer(), numProposals, func(atx *types.ActivationTx) (*types.VerifiedActivationTx, error) {
		n := rng.Uint64()
		if n > max {
			max = n
		}
		return atx.Verify(n, 1)
	})
	activeSet := types.ToATXIDs(atxes)
	totalNumTXs := 100
	tids, _ := addBatch(t, tcs, totalNumTXs)
	// make sure proposals have overlapping transactions
	step := totalNumTXs / numProposals
	proposals := createProposalsDupTXs(t, lid, signers, activeSet, types.EmptyLayerHash, step, tids)
	got, executed, err := tcs.GenerateBlock(context.Background(), lid, proposals)
	require.NoError(t, err)
	require.False(t, executed)
	require.NotEqual(t, types.EmptyBlockID, got.ID())
	require.Equal(t, lid, got.LayerIndex)
	require.Len(t, got.TxIDs, totalNumTXs)
	require.Equal(t, max, got.TickHeight)
}

func TestGenerateBlock_EmptyProposals(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.GetEffectiveGenesis().Add(20)
	signers, atxes := createATXs(t, tcs.cdb, (lid.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	epochData := &types.EpochData{
		ActiveSet: activeSet,
		Beacon:    types.RandomBeacon(),
	}
	proposals := make([]*types.Proposal, 0, numProposals)
	for i := 0; i < numProposals; i++ {
		p := createProposal(t, epochData, lid, activeSet[i], signers[i], nil, 1)
		proposals = append(proposals, p)
	}
	got, executed, err := tcs.GenerateBlock(context.Background(), lid, proposals)
	require.NoError(t, err)
	require.False(t, executed)
	require.NotEqual(t, types.EmptyBlockID, got.ID())
	require.Equal(t, lid, got.LayerIndex)
	require.Empty(t, got.TxIDs)
	require.Len(t, got.Rewards, numProposals)
	// numUint is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1 eligibility
	// the expected weight for each eligibility is `numUnit` * 1/3
	expWeight := util.WeightFromInt64(numUnit * 1 / 3)
	checkRewards(t, atxes, expWeight, got.Rewards)
}

func TestGenerateBlock_SameCoinbase(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.GetEffectiveGenesis().Add(100)
	totalNumTXs := 1000
	tids, _ := addBatch(t, tcs, totalNumTXs)
	signers, atxes := createATXs(t, tcs.cdb, (lid.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	epochData := &types.EpochData{
		ActiveSet: activeSet,
		Beacon:    types.RandomBeacon(),
	}
	atxID := activeSet[0]
	proposal1 := createProposal(t, epochData, lid, atxID, signers[0], tids[0:500], 1)
	proposal2 := createProposal(t, epochData, lid, atxID, signers[0], tids[400:], 1)
	proposals := []*types.Proposal{proposal1, proposal2}
	got, executed, err := tcs.GenerateBlock(context.Background(), lid, proposals)
	require.NoError(t, err)
	require.False(t, executed)
	require.NotEqual(t, types.EmptyBlockID, got.ID())
	require.Equal(t, lid, got.LayerIndex)
	require.Len(t, got.TxIDs, totalNumTXs)

	// numUint is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1 eligibility
	// the expected weight for each eligibility is `numUnit` * 1/3
	// since there are two proposals for the same coinbase, the final weight is `numUnit` * 1/3 * 2
	expWeight := util.WeightFromInt64(numUnit * 1 / 3 * 2)
	checkRewards(t, atxes[0:1], expWeight, got.Rewards)
}

func TestGenerateBlock_EmptyATXID(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	totalNumTXs := 1000
	tids, _ := addBatch(t, tcs, totalNumTXs)
	signers, atxes := createATXs(t, tcs.cdb, (lid.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	// make sure proposals have overlapping transactions
	step := totalNumTXs / numProposals
	proposals := createProposalsDupTXs(t, lid, signers, activeSet, types.EmptyLayerHash, step, tids)
	// set the last proposal ID to be empty
	types.SortProposals(proposals)
	proposals[numProposals-1].AtxID = *types.EmptyATXID

	got, executed, err := tcs.GenerateBlock(context.Background(), lid, proposals)
	require.ErrorIs(t, err, errInvalidATXID)
	require.False(t, executed)
	require.Nil(t, got)
}

func TestGenerateBlocks_MultipleEligibilities(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.GetEffectiveGenesis().Add(100)
	tids, _ := addBatch(t, tcs, 1000)
	signers, atxes := createATXs(t, tcs.cdb, (lid.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	epochData := &types.EpochData{
		ActiveSet: activeSet,
		Beacon:    types.RandomBeacon(),
	}
	proposals := []*types.Proposal{
		createProposal(t, epochData, lid, atxes[0].ID(), signers[0], tids, 2),
		createProposal(t, epochData, lid, atxes[1].ID(), signers[1], tids, 1),
		createProposal(t, epochData, lid, atxes[2].ID(), signers[2], tids, 5),
	}
	got, executed, err := tcs.GenerateBlock(context.Background(), lid, proposals)
	require.NoError(t, err)
	require.False(t, executed)
	require.Len(t, got.Rewards, len(proposals))
	sort.Slice(proposals, func(i, j int) bool {
		cbi := types.GenerateAddress(proposals[i].SmesherID().Bytes())
		cbj := types.GenerateAddress(proposals[j].SmesherID().Bytes())
		return bytes.Compare(cbi.Bytes(), cbj.Bytes()) < 0
	})
	totalWeight := util.WeightFromUint64(0)
	for i, r := range got.Rewards {
		require.Equal(t, types.GenerateAddress(proposals[i].SmesherID().Bytes()), r.Coinbase)
		weight := util.WeightFromNumDenom(r.Weight.Num, r.Weight.Denom)
		// numUint is the ATX weight. eligible slots per epoch is 3 for each atx
		// the expected weight for each eligibility is `numUnit` * 1/3
		require.Equal(t, util.WeightFromInt64(numUnit*1/3*int64(len(proposals[i].EligibilityProofs))), weight)
		totalWeight.Add(weight)
	}
	require.Equal(t, util.WeightFromInt64(numUnit*8*1/3), totalWeight)
}

func TestGenerateBlocks_MissingHeader(t *testing.T) {
	tcs := createConservativeState(t)
	tx := types.Transaction{
		RawTx: types.NewRawTx([]byte{1, 1, 1}),
	}
	require.NoError(t, tcs.AddToDB(&tx))

	_, _, err := tcs.GenerateBlock(
		context.Background(),
		types.NewLayerID(1), []*types.Proposal{
			{
				InnerProposal: types.InnerProposal{
					TxIDs: []types.TransactionID{tx.ID},
				},
			},
		})
	require.Error(t, err)
}

func TestGenerateBlock_AllNodesMissingMeshHash(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.NewLayerID(1333)
	totalNumTXs := 100
	tids, _ := addBatch(t, tcs, totalNumTXs)
	step := totalNumTXs / numProposals
	signers, atxes := createATXs(t, tcs.cdb, (lid.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	proposals := createProposalsDupTXs(t, lid, signers, activeSet, types.EmptyLayerHash, step, tids)
	got, executed, err := tcs.GenerateBlock(context.Background(), lid, proposals)
	require.NoError(t, err)
	require.False(t, executed)
	require.NotEqual(t, types.EmptyBlockID, got.ID())
	require.Equal(t, lid, got.LayerIndex)
	require.Len(t, got.TxIDs, totalNumTXs)
}

func TestGenerateBlock_PrunedByGasLimit(t *testing.T) {
	totalNumTXs := 100
	expSize := totalNumTXs / 3
	gasLimit := uint64(expSize) * defaultGas
	tcs := createTestState(t, gasLimit)
	lid := types.NewLayerID(1333)
	tids, _ := addBatch(t, tcs, totalNumTXs)
	step := totalNumTXs / numProposals
	signers, atxes := createATXs(t, tcs.cdb, (lid.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	proposals := createProposalsDupTXs(t, lid, signers, activeSet, types.EmptyLayerHash, step, tids)
	got, executed, err := tcs.GenerateBlock(context.Background(), lid, proposals)
	require.NoError(t, err)
	require.False(t, executed)
	require.NotEqual(t, types.EmptyBlockID, got.ID())
	require.Equal(t, lid, got.LayerIndex)
	require.Len(t, got.TxIDs, expSize)
}

func TestGenerateBlock_NodeHasDifferentMesh(t *testing.T) {
	tcs := createConservativeState(t)
	meshHash := types.RandomHash()
	lid := types.NewLayerID(1333)
	proposals := make([]*types.Proposal, 0, numProposals)
	for i := 0; i < numProposals; i++ {
		p := types.GenLayerProposal(lid, nil)
		p.MeshHash = meshHash
		proposals = append(proposals, p)
	}
	require.NoError(t, layers.SetHashes(tcs.cdb, lid.Sub(1), types.RandomHash(), types.RandomHash()))
	got, executed, err := tcs.GenerateBlock(context.Background(), lid, proposals)
	require.ErrorIs(t, err, errNodeHasBadMeshHash)
	require.Nil(t, got)
	require.False(t, executed)
}

func TestGenerateBlock_OwnNodeMissingMeshHash(t *testing.T) {
	tcs := createConservativeState(t)
	meshHash := types.RandomHash()
	lid := types.NewLayerID(1333)
	proposals := make([]*types.Proposal, 0, numProposals)
	for i := 0; i < numProposals; i++ {
		p := types.GenLayerProposal(lid, nil)
		p.MeshHash = meshHash
		proposals = append(proposals, p)
	}
	got, executed, err := tcs.GenerateBlock(context.Background(), lid, proposals)
	require.ErrorIs(t, err, errNodeHasBadMeshHash)
	require.Nil(t, got)
	require.False(t, executed)
}

func TestSelectProposalTXs(t *testing.T) {
	tcs := createConservativeState(t)
	totalNumTXs := 2 * numTXsInProposal
	lid := types.NewLayerID(97)
	bid := types.BlockID{100}
	for i := 0; i < totalNumTXs; i++ {
		signer := signing.NewEdSigner()
		addr := types.GenerateAddress(signer.PublicKey().Bytes())
		tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
		tcs.mvm.EXPECT().GetNonce(addr).Return(types.Nonce{Counter: 1}, nil).Times(1)
		tx1 := newTx(t, 4, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.TODO(), tx1))
		// all the TXs with nonce 1 are pending in database
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx1.ID}))
		tx2 := newTx(t, 6, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.TODO(), tx2))
	}

	got := tcs.SelectProposalTXs(lid, 1)
	require.Len(t, got, numTXsInProposal)

	// the second call should have different result than the first, as the transactions should be
	// randomly selected
	got2 := tcs.SelectProposalTXs(lid, 1)
	require.Len(t, got2, numTXsInProposal)
	require.NotSubset(t, got, got2)
	require.NotSubset(t, got2, got)
}

func TestSelectProposalTXs_ExhaustGas(t *testing.T) {
	totalNumTXs := 2 * numTXsInProposal
	lid := types.NewLayerID(97)
	bid := types.BlockID{100}
	expSize := numTXsInProposal / 2
	gasLimit := defaultGas * uint64(expSize)
	tcs := createTestState(t, gasLimit)
	for i := 0; i < totalNumTXs; i++ {
		signer := signing.NewEdSigner()
		addr := types.GenerateAddress(signer.PublicKey().Bytes())
		tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
		tcs.mvm.EXPECT().GetNonce(addr).Return(types.Nonce{}, nil).Times(1)
		tx1 := newTx(t, 0, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.TODO(), tx1))
		// all the TXs with nonce 0 are pending in database
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx1.ID}))
		tx2 := newTx(t, 1, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.TODO(), tx2))
	}
	got := tcs.SelectProposalTXs(lid, 1)
	require.Len(t, got, expSize)
}

func TestSelectProposalTXs_ExhaustMemPool(t *testing.T) {
	if util.IsWindows() {
		t.Skip("Skipping test in Windows (https://github.com/spacemeshos/go-spacemesh/issues/3624)")
	}
	tcs := createConservativeState(t)
	totalNumTXs := numTXsInProposal - 1
	lid := types.NewLayerID(97)
	bid := types.BlockID{100}
	expected := make([]types.TransactionID, 0, totalNumTXs)
	for i := 0; i < totalNumTXs; i++ {
		signer := signing.NewEdSigner()
		addr := types.GenerateAddress(signer.PublicKey().Bytes())
		tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
		tcs.mvm.EXPECT().GetNonce(addr).Return(types.Nonce{}, nil).Times(1)
		tx1 := newTx(t, 0, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.TODO(), tx1))
		// all the TXs with nonce 0 are pending in database
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx1.ID}))
		tx2 := newTx(t, 1, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.TODO(), tx2))
		expected = append(expected, tx2.ID)
	}
	got := tcs.SelectProposalTXs(lid, 1)
	// no reshuffling happens when mempool is exhausted. two list should be exactly the same
	require.Equal(t, expected, got)

	// the second call should have the same result since both exhaust the mempool
	got2 := tcs.SelectProposalTXs(lid, 1)
	require.Equal(t, got, got2)
}

func TestSelectProposalTXs_SamePrincipal(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	totalNumTXs := numTXsInProposal * 2
	numInBlock := numTXsInProposal
	lid := types.NewLayerID(97)
	bid := types.BlockID{100}
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(types.Nonce{}, nil).Times(1)
	for i := 0; i < numInBlock; i++ {
		tx := newTx(t, uint64(i), defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.TODO(), tx))
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx.ID}))
	}
	expected := make([]types.TransactionID, 0, numTXsInProposal)
	for i := 0; i < totalNumTXs; i++ {
		tx := newTx(t, uint64(numInBlock+i), defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.TODO(), tx))
		if i < numTXsInProposal {
			expected = append(expected, tx.ID)
		}
	}
	got := tcs.SelectProposalTXs(lid, 1)
	require.Equal(t, expected, got)
}

func TestSelectProposalTXs_TwoPrincipals(t *testing.T) {
	const (
		numInProposal = 30
		numTXs        = numInProposal * 2
		numInDBs      = numInProposal
	)
	tcs := createConservativeState(t)
	signer1 := signing.NewEdSigner()
	addr1 := types.GenerateAddress(signer1.PublicKey().Bytes())
	signer2 := signing.NewEdSigner()
	addr2 := types.GenerateAddress(signer2.PublicKey().Bytes())
	lid := types.NewLayerID(97)
	bid := types.BlockID{100}
	tcs.mvm.EXPECT().GetBalance(addr1).Return(defaultBalance*100, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr1).Return(types.Nonce{}, nil).Times(1)
	tcs.mvm.EXPECT().GetBalance(addr2).Return(defaultBalance*100, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr2).Return(types.Nonce{}, nil).Times(1)
	allTXs := make(map[types.TransactionID]*types.Transaction)
	for i := 0; i < numInDBs; i++ {
		tx := newTx(t, uint64(i), defaultAmount, defaultFee, signer1)
		require.NoError(t, tcs.AddToCache(context.TODO(), tx))
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx.ID}))
		allTXs[tx.ID] = tx
		tx = newTx(t, uint64(i), defaultAmount, defaultFee, signer2)
		require.NoError(t, tcs.AddToCache(context.TODO(), tx))
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx.ID}))
		allTXs[tx.ID] = tx
	}
	for i := 0; i < numTXs; i++ {
		tx := newTx(t, uint64(numInDBs+i), defaultAmount, defaultFee, signer1)
		require.NoError(t, tcs.AddToCache(context.TODO(), tx))
		allTXs[tx.ID] = tx
		tx = newTx(t, uint64(numInDBs+i), defaultAmount, defaultFee, signer2)
		require.NoError(t, tcs.AddToCache(context.TODO(), tx))
		allTXs[tx.ID] = tx
	}
	got := tcs.SelectProposalTXs(lid, 1)
	// the odds of picking just one principal is 2^30
	chosen := make(map[types.Address][]*types.Transaction)
	for _, tid := range got {
		tx := allTXs[tid]
		chosen[tx.Principal] = append(chosen[tx.Principal], tx)
	}
	require.Len(t, chosen, 2)
	require.Contains(t, chosen, addr1)
	require.Contains(t, chosen, addr2)
	// making sure nonce values are in order
	for i, tx := range chosen[addr1] {
		require.Equal(t, uint64(i+numInDBs), tx.Nonce.Counter)
	}
	for i, tx := range chosen[addr2] {
		require.Equal(t, uint64(i+numInDBs), tx.Nonce.Counter)
	}
}

func TestGetProjection(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(types.Nonce{Counter: nonce}, nil).Times(1)
	tx1 := newTx(t, nonce, defaultAmount, defaultFee, signer)
	require.NoError(t, tcs.AddToCache(context.TODO(), tx1))
	require.NoError(t, tcs.LinkTXsWithBlock(types.NewLayerID(10), types.BlockID{100}, []types.TransactionID{tx1.ID}))
	tx2 := newTx(t, nonce+1, defaultAmount, defaultFee, signer)
	require.NoError(t, tcs.AddToCache(context.TODO(), tx2))

	got, balance := tcs.GetProjection(addr)
	require.EqualValues(t, nonce+2, got)
	require.EqualValues(t, defaultBalance-2*(defaultAmount+defaultFee*defaultGas), balance)
}

func TestAddToCache(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(types.Nonce{Counter: nonce}, nil).Times(1)
	tx := newTx(t, nonce, defaultAmount, defaultFee, signer)
	require.NoError(t, tcs.AddToCache(context.TODO(), tx))
	has := tcs.cache.Has(tx.ID)
	require.True(t, has)
	got, err := transactions.Get(tcs.cdb, tx.ID)
	require.NoError(t, err)
	require.Equal(t, *tx, got.Transaction)
}

func TestAddToCache_BadNonceNotPersisted(t *testing.T) {
	tcs := createConservativeState(t)
	tx := &types.Transaction{
		RawTx: types.NewRawTx([]byte{1, 1, 1}),
		TxHeader: &types.TxHeader{
			Principal: types.Address{1, 1},
			Nonce:     types.Nonce{Counter: 100},
		},
	}
	tcs.mvm.EXPECT().GetBalance(tx.Principal).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(tx.Principal).Return(types.Nonce{Counter: tx.Nonce.Counter + 1}, nil).Times(1)
	require.ErrorIs(t, tcs.AddToCache(context.TODO(), tx), errBadNonce)
	checkTXNotInDB(t, tcs.cdb, tx.ID)
}

func TestAddToCache_NonceGap(t *testing.T) {
	tcs := createConservativeState(t)
	tx := &types.Transaction{
		RawTx: types.NewRawTx([]byte{1, 1, 1}),
		TxHeader: &types.TxHeader{
			Principal: types.Address{1, 1},
			Nonce:     types.Nonce{Counter: 100},
		},
	}
	tcs.mvm.EXPECT().GetBalance(tx.Principal).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(tx.Principal).Return(types.Nonce{Counter: tx.Nonce.Counter - 2}, nil).Times(1)
	require.NoError(t, tcs.AddToCache(context.TODO(), tx))
	require.True(t, tcs.cache.Has(tx.ID))
	require.False(t, tcs.cache.MoreInDB(tx.Principal))
	checkTXStateFromDB(t, tcs.cdb, []*types.MeshTransaction{{Transaction: *tx}}, types.MEMPOOL)
}

func TestAddToCache_InsufficientBalance(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultAmount, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(types.Nonce{Counter: nonce}, nil).Times(1)
	tx := newTx(t, nonce, defaultAmount, defaultFee, signer)
	require.NoError(t, tcs.AddToCache(context.TODO(), tx))
	checkNoTX(t, tcs.cache, tx.ID)
	require.True(t, tcs.cache.MoreInDB(addr))
	checkTXStateFromDB(t, tcs.cdb, []*types.MeshTransaction{{Transaction: *tx}}, types.MEMPOOL)
}

func TestAddToCache_TooManyForOneAccount(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	tcs.mvm.EXPECT().GetBalance(addr).Return(uint64(math.MaxUint64), nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(types.Nonce{Counter: nonce}, nil).Times(1)
	mtxs := make([]*types.MeshTransaction, 0, maxTXsPerAcct+1)
	for i := 0; i <= maxTXsPerAcct; i++ {
		tx := newTx(t, nonce+uint64(i), defaultAmount, defaultFee, signer)
		mtxs = append(mtxs, &types.MeshTransaction{Transaction: *tx})
		require.NoError(t, tcs.AddToCache(context.TODO(), tx))
	}
	require.True(t, tcs.cache.MoreInDB(addr))
	checkTXStateFromDB(t, tcs.cdb, mtxs, types.MEMPOOL)
}

func TestGetMeshTransaction(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(types.Nonce{Counter: nonce}, nil).Times(1)
	tx := newTx(t, nonce, defaultAmount, defaultFee, signer)
	require.NoError(t, tcs.AddToCache(context.TODO(), tx))
	mtx, err := tcs.GetMeshTransaction(tx.ID)
	require.NoError(t, err)
	require.Equal(t, types.MEMPOOL, mtx.State)

	lid := types.NewLayerID(10)
	pid := types.ProposalID{1, 2, 3}
	require.NoError(t, tcs.LinkTXsWithProposal(lid, pid, []types.TransactionID{tx.ID}))
	mtx, err = tcs.GetMeshTransaction(tx.ID)
	require.NoError(t, err)
	require.Equal(t, types.MEMPOOL, mtx.State)

	bid := types.BlockID{2, 3, 4}
	require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx.ID}))
	mtx, err = tcs.GetMeshTransaction(tx.ID)
	require.NoError(t, err)
	require.Equal(t, types.MEMPOOL, mtx.State)
}

func Test_ApplyLayer_UpdateHeader(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.NewLayerID(1)

	signer := signing.NewEdSigner()
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(types.Nonce{Counter: nonce}, nil).Times(1)
	tx := newTx(t, nonce, defaultAmount, defaultFee, signer)
	hdrless := *tx
	hdrless.TxHeader = nil

	// the hdrless tx is saved via syncing from blocks
	require.NoError(t, transactions.Add(tcs.cdb, &hdrless, time.Now()))
	got, err := transactions.Get(tcs.cdb, tx.ID)
	require.NoError(t, err)
	require.Nil(t, got.TxHeader)

	coinbase := types.GenerateAddress(types.RandomBytes(types.AddressLength))
	weight := util.WeightFromFloat64(200.56)
	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{
			LayerIndex: lid,
			Rewards: []types.AnyReward{
				{Coinbase: coinbase, Weight: types.RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()}},
			},
			TxIDs: []types.TransactionID{tx.ID},
		})
	tcs.mvm.EXPECT().GetBalance(tx.Principal).Return(defaultBalance-(defaultAmount+defaultFee), nil)
	tcs.mvm.EXPECT().GetNonce(tx.Principal).Return(types.Nonce{Counter: nonce + 1}, nil)
	tcs.mvm.EXPECT().Apply(vm.ApplyContext{Layer: lid, Block: block.ID()}, gomock.Any(), block.Rewards).DoAndReturn(
		func(ctx vm.ApplyContext, got []types.Transaction, _ []types.AnyReward) ([]types.TransactionID, []types.TransactionWithResult, error) {
			matchReceived(t, []types.Transaction{*tx}, got)
			rst := []types.TransactionWithResult{
				{
					Transaction: *tx,
					TransactionResult: types.TransactionResult{
						Layer: ctx.Layer,
						Block: ctx.Block,
					},
				},
			}
			return nil, rst, nil
		}).Times(1)

	require.NoError(t, tcs.ApplyLayer(context.TODO(), block.LayerIndex, block))

	got, err = transactions.Get(tcs.cdb, tx.ID)
	require.NoError(t, err)
	require.NotNil(t, got.TxHeader)
}

func TestApplyLayer(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.NewLayerID(1)
	ids, txs := addBatch(t, tcs, numTXs)
	coinbase := types.GenerateAddress(types.RandomBytes(types.AddressLength))
	weight := util.WeightFromFloat64(200.56)
	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{
			LayerIndex: lid,
			Rewards: []types.AnyReward{
				{Coinbase: coinbase, Weight: types.RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()}},
			},
			TxIDs: ids,
		})
	for _, tx := range txs {
		tcs.mvm.EXPECT().GetBalance(tx.Principal).Return(defaultBalance-(defaultAmount+defaultFee), nil)
		tcs.mvm.EXPECT().GetNonce(tx.Principal).Return(types.Nonce{Counter: nonce + 1}, nil)
	}
	tcs.mvm.EXPECT().Apply(vm.ApplyContext{Layer: lid, Block: block.ID()}, gomock.Any(), block.Rewards).DoAndReturn(
		func(ctx vm.ApplyContext, got []types.Transaction, _ []types.AnyReward) ([]types.TransactionID, []types.TransactionWithResult, error) {
			matchReceived(t, txs, got)
			var rst []types.TransactionWithResult
			for _, tx := range txs {
				rst = append(rst, types.TransactionWithResult{
					Transaction: tx,
					TransactionResult: types.TransactionResult{
						Layer: ctx.Layer,
						Block: ctx.Block,
					},
				})
			}
			return nil, rst, nil
		}).Times(1)

	require.NoError(t, tcs.ApplyLayer(context.TODO(), block.LayerIndex, block))

	for _, id := range ids {
		mtx, err := transactions.Get(tcs.cdb, id)
		require.NoError(t, err)
		require.Equal(t, types.APPLIED, mtx.State)
		require.Equal(t, lid, mtx.LayerID)
		require.Equal(t, block.ID(), mtx.BlockID)
	}
}

func TestApplyEmptyLayer(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.NewLayerID(1)
	ids, _ := addBatch(t, tcs, numTXs)
	require.NoError(t, tcs.LinkTXsWithBlock(lid, types.BlockID{1, 2, 3}, ids))
	mempoolTxs := tcs.SelectProposalTXs(lid, 2)
	require.Empty(t, mempoolTxs)

	tcs.mvm.EXPECT().Apply(vm.ApplyContext{Layer: lid, Block: types.EmptyBlockID}, nil, nil).Return(nil, nil, nil)
	require.NoError(t, tcs.ApplyLayer(context.TODO(), lid, nil))
	mempoolTxs = tcs.SelectProposalTXs(lid, 2)
	require.Len(t, mempoolTxs, len(ids))
}

func TestApplyLayer_TXsFailedVM(t *testing.T) {
	const numFailed = 2
	tcs := createConservativeState(t)
	lid := types.NewLayerID(1)
	ids, txs := addBatch(t, tcs, numTXs)
	for _, tx := range txs {
		principal := tx.Principal
		tcs.mvm.EXPECT().GetBalance(principal).Return(defaultBalance-(tx.Spending()), nil).Times(1)
		tcs.mvm.EXPECT().GetNonce(principal).Return(types.Nonce{Counter: nonce + 1}, nil).Times(1)
	}
	coinbase := types.GenerateAddress(types.RandomBytes(types.AddressLength))
	weight := util.WeightFromFloat64(200.56)
	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{
			LayerIndex: lid,
			Rewards: []types.AnyReward{
				{Coinbase: coinbase, Weight: types.RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()}},
			},
			TxIDs: ids,
		})
	tcs.mvm.EXPECT().Apply(vm.ApplyContext{Layer: lid, Block: block.ID()}, gomock.Any(), block.Rewards).DoAndReturn(
		func(ctx vm.ApplyContext, got []types.Transaction, _ []types.AnyReward) ([]types.Transaction, []types.TransactionWithResult, error) {
			matchReceived(t, txs, got)
			var (
				inefective []types.Transaction
				rst        []types.TransactionWithResult
			)
			for i, tx := range txs {
				if i < numFailed {
					inefective = append(inefective, tx)
				} else {
					rst = append(rst, types.TransactionWithResult{
						Transaction: tx,
						TransactionResult: types.TransactionResult{
							Layer: ctx.Layer,
							Block: ctx.Block,
						},
					})
				}
			}
			return inefective, rst, nil
		}).Times(1)

	require.NoError(t, tcs.ApplyLayer(context.TODO(), block.LayerIndex, block))

	for i, id := range ids {
		mtx, err := transactions.Get(tcs.cdb, id)
		require.NoError(t, err)
		if i < numFailed {
			require.Equal(t, types.MEMPOOL, mtx.State)
			require.Equal(t, types.EmptyBlockID, mtx.BlockID)
			require.Equal(t, types.LayerID{}, mtx.LayerID)
		} else {
			require.Equal(t, types.APPLIED, mtx.State)
			require.Equal(t, lid, mtx.LayerID)
			require.Equal(t, block.ID(), mtx.BlockID)
		}
	}
}

func TestApplyLayer_VMError(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.NewLayerID(1)
	ids, txs := addBatch(t, tcs, numTXs)
	coinbase := types.GenerateAddress(types.RandomBytes(types.AddressLength))
	weight := util.WeightFromFloat64(200.56)
	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{
			LayerIndex: lid,
			Rewards: []types.AnyReward{
				{Coinbase: coinbase, Weight: types.RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()}},
			},
			TxIDs: ids,
		})
	errVM := errors.New("vm error")
	tcs.mvm.EXPECT().Apply(vm.ApplyContext{Layer: lid, Block: block.ID()}, gomock.Any(), block.Rewards).DoAndReturn(
		func(_ vm.ApplyContext, got []types.Transaction, _ []types.AnyReward) ([]types.Transaction, []types.TransactionWithResult, error) {
			matchReceived(t, txs, got)
			return nil, nil, errVM
		}).Times(1)

	require.ErrorIs(t, tcs.ApplyLayer(context.TODO(), block.LayerIndex, block), errVM)

	for _, id := range ids {
		mtx, err := transactions.Get(tcs.cdb, id)
		require.NoError(t, err)
		require.Equal(t, types.MEMPOOL, mtx.State)
		require.Equal(t, types.EmptyBlockID, mtx.BlockID)
		require.Equal(t, types.LayerID{}, mtx.LayerID)
	}
}

func TestConsistentHandling(t *testing.T) {
	// there are two different workflows for transactions
	// 1. receive gossiped transaction and verify it immediately
	// 2. receive synced transaction and delay verification
	// this test is meant to ensure that both of them will result in a consistent
	// conservative cache state

	instances := []*testConState{
		createConservativeState(t),
		createConservativeState(t),
	}

	rng := rand.New(rand.NewSource(101))
	signers := make([]*signing.EdSigner, 30)
	nonces := make([]uint64, len(signers))
	for i := range signers {
		signers[i] = signing.NewEdSignerFromRand(rng)
	}
	for _, instance := range instances {
		instance.mvm.EXPECT().GetBalance(gomock.Any()).Return(defaultBalance, nil).AnyTimes()
		instance.mvm.EXPECT().GetNonce(gomock.Any()).Return(types.Nonce{}, nil).AnyTimes()
	}
	for lid := 1; lid < 10; lid++ {
		txs := make([]*types.Transaction, 100)
		ids := make([]types.TransactionID, len(txs))
		raw := make([]types.Transaction, len(txs))
		verified := make([]types.Transaction, len(txs))
		for i := range txs {
			signer := rng.Intn(len(signers))
			txs[i] = newTx(t, nonces[signer], 1, 1, signers[signer])
			nonces[signer]++
			noheader := *(txs[i])
			noheader.TxHeader = nil
			ids[i] = txs[i].ID
			raw[i] = noheader
			verified[i] = *txs[i]

			req := smocks.NewMockValidationRequest(gomock.NewController(t))
			req.EXPECT().Parse().Times(1).Return(txs[i].TxHeader, nil)
			req.EXPECT().Verify().Times(1).Return(true)
			instances[0].mvm.EXPECT().Validation(txs[i].RawTx).Times(1).Return(req)

			failed := smocks.NewMockValidationRequest(gomock.NewController(t))
			failed.EXPECT().Parse().Times(1).Return(nil, errors.New("test"))
			instances[1].mvm.EXPECT().Validation(txs[i].RawTx).Times(1).Return(failed)

			require.Equal(t, pubsub.ValidationAccept, instances[0].handler().HandleGossipTransaction(context.TODO(), "", txs[i].Raw))
			require.NoError(t, instances[1].handler().HandleBlockTransaction(context.TODO(), txs[i].Raw))
		}
		block := types.NewExistingBlock(types.BlockID{byte(lid)},
			types.InnerBlock{
				LayerIndex: types.NewLayerID(uint32(lid)),
				TxIDs:      ids,
			},
		)
		results := make([]types.TransactionWithResult, len(txs))
		for i, tx := range txs {
			results[i] = types.TransactionWithResult{
				Transaction: *tx,
				TransactionResult: types.TransactionResult{
					Layer: block.LayerIndex,
					Block: block.ID(),
				},
			}
		}
		instances[0].mvm.EXPECT().Apply(vm.ApplyContext{
			Layer: block.LayerIndex,
			Block: block.ID(),
		}, verified, block.Rewards).Return(nil, results, nil).Times(1)
		instances[1].mvm.EXPECT().Apply(vm.ApplyContext{
			Layer: block.LayerIndex,
			Block: block.ID(),
		}, raw, block.Rewards).Return(nil, results, nil).Times(1)
		for _, instance := range instances {
			require.NoError(t, instance.ApplyLayer(context.TODO(), block.LayerIndex, block))
			require.NoError(t, layers.SetApplied(instance.cdb, block.LayerIndex, block.ID()))
		}
	}
}

func matchReceived(tb testing.TB, expected []types.Transaction, got []types.Transaction) {
	tb.Helper()
	require.Len(tb, got, len(expected))
	for i := range expected {
		require.Equal(tb, expected[i].RawTx, got[i].GetRaw())
	}
}
