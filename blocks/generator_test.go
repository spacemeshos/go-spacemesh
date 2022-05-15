package blocks

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/transaction"
)

const (
	maxFee  = 2000
	numUint = 12
)

func testConfig() RewardConfig {
	return RewardConfig{
		BaseReward: 5055,
	}
}

type testGenerator struct {
	*Generator
	ctrl      *gomock.Controller
	mockMesh  *mocks.MockmeshProvider
	mockATXDB *mocks.MockatxProvider
	mockTXs   *mocks.MocktxProvider
}

func createTestGenerator(t *testing.T) *testGenerator {
	ctrl := gomock.NewController(t)
	tg := &testGenerator{
		ctrl:      ctrl,
		mockMesh:  mocks.NewMockmeshProvider(ctrl),
		mockATXDB: mocks.NewMockatxProvider(ctrl),
		mockTXs:   mocks.NewMocktxProvider(ctrl),
	}
	tg.Generator = NewGenerator(tg.mockATXDB, tg.mockMesh, tg.mockTXs, WithGeneratorLogger(logtest.New(t)), WithConfig(testConfig()))
	return tg
}

func createTransactions(t testing.TB, numOfTxs int) (uint64, []types.TransactionID, []*types.MeshTransaction) {
	t.Helper()
	var totalFee uint64
	txs := make([]*types.MeshTransaction, 0, numOfTxs)
	txIDs := make([]types.TransactionID, 0, numOfTxs)
	for i := 0; i < numOfTxs; i++ {
		fee := uint64(rand.Intn(maxFee))
		tx, err := transaction.GenerateCallTransaction(signing.NewEdSigner(), types.HexToAddress("1"), 1, 10, 100, fee)
		require.NoError(t, err)
		totalFee += fee
		txs = append(txs, &types.MeshTransaction{Transaction: *tx})
		txIDs = append(txIDs, tx.ID())
	}
	return totalFee, txIDs, txs
}

func createProposalsWithOverlappingTXs(t *testing.T, layerID types.LayerID, numProposals int, txIDs []types.TransactionID) (
	map[types.ATXID]*types.ActivationTx, []*types.Proposal,
) {
	t.Helper()
	require.Zero(t, len(txIDs)%numProposals)
	pieSize := len(txIDs) / numProposals
	atxs := make(map[types.ATXID]*types.ActivationTx)
	proposals := make([]*types.Proposal, 0, numProposals)
	for i := 0; i < numProposals; i++ {
		from := i * pieSize
		to := from + (2 * pieSize) // every adjacent proposal will overlap pieSize of TXs
		if to > len(txIDs) {
			to = len(txIDs)
		}
		thisPie := txIDs[from:to]
		atx, p := createProposalWithATX(t, layerID, thisPie)
		atxs[atx.ID()] = atx
		proposals = append(proposals, p)
	}
	return atxs, proposals
}

func createProposalWithATX(t *testing.T, layerID types.LayerID, txIDs []types.TransactionID) (*types.ActivationTx, *types.Proposal) {
	return createProposalATXWithCoinbase(t, layerID, txIDs, signing.NewEdSigner())
}

func createProposalATXWithCoinbase(t *testing.T, layerID types.LayerID, txIDs []types.TransactionID, signer *signing.EdSigner) (*types.ActivationTx, *types.Proposal) {
	t.Helper()
	address := types.BytesToAddress(signer.PublicKey().Bytes())
	nipostChallenge := types.NIPostChallenge{
		NodeID:     types.BytesToNodeID(signer.PublicKey().Bytes()),
		StartTick:  1,
		EndTick:    2,
		PubLayerID: layerID,
	}
	atx := types.NewActivationTx(nipostChallenge, address, nil, numUint, nil)

	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot: types.InnerBallot{
					AtxID:             atx.ID(),
					LayerIndex:        layerID,
					EligibilityProofs: []types.VotingEligibilityProof{{}},
				},
			},
			TxIDs: txIDs,
		},
	}
	p.Ballot.Signature = signer.Sign(p.Ballot.Bytes())
	p.Signature = signer.Sign(p.Bytes())
	require.NoError(t, p.Initialize())
	return atx, p
}

func Test_GenerateBlock(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	totalFees, txIDs, txs := createTransactions(t, numTXs)
	atxs, proposals := createProposalsWithOverlappingTXs(t, layerID, numProposals, txIDs)
	tg.mockTXs.EXPECT().GetMeshTransactions(gomock.Any()).DoAndReturn(
		func(got []types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
			assert.ElementsMatch(t, got, txIDs)
			return txs, nil
		}).Times(1)
	tg.mockATXDB.EXPECT().GetAtxHeader(gomock.Any()).DoAndReturn(
		func(id types.ATXID) (*types.ActivationTxHeader, error) {
			return atxs[id].ActivationTxHeader, nil
		}).Times(numProposals)
	block, err := tg.GenerateBlock(context.TODO(), layerID, proposals)
	require.NoError(t, err)
	assert.NotEqual(t, types.EmptyBlockID, block.ID())

	assert.Equal(t, layerID, block.LayerIndex)
	assert.Len(t, block.TxIDs, numTXs)

	totalRewards := totalFees + testConfig().BaseReward
	unitReward := totalRewards / uint64(numProposals)
	unitLayerReward := testConfig().BaseReward / uint64(numProposals)

	types.SortProposals(proposals)
	for i, r := range block.Rewards {
		assert.Equal(t, unitReward, r.Amount)
		assert.Equal(t, unitLayerReward, r.LayerReward)
		assert.Equal(t, types.BytesToAddress(proposals[i].SmesherID().Bytes()), r.Address)
		assert.Equal(t, proposals[i].SmesherID().Bytes(), r.SmesherID[:])
	}

	// make sure the rewards remainder, tho ignored, is correct
	totalRemainder := totalRewards % uint64(numProposals)
	assert.Equal(t, totalRewards, unitReward*uint64(numProposals)+totalRemainder)
}

func Test_GenerateBlockStableBlockID(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	_, txIDs, txs := createTransactions(t, numTXs)
	atxs, proposals := createProposalsWithOverlappingTXs(t, layerID, numProposals, txIDs)
	tg.mockTXs.EXPECT().GetMeshTransactions(gomock.Any()).DoAndReturn(
		func(got []types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
			assert.ElementsMatch(t, got, txIDs)
			return txs, nil
		}).Times(2)
	tg.mockATXDB.EXPECT().GetAtxHeader(gomock.Any()).DoAndReturn(
		func(id types.ATXID) (*types.ActivationTxHeader, error) {
			return atxs[id].ActivationTxHeader, nil
		}).Times(numProposals * 2)
	block, err := tg.GenerateBlock(context.TODO(), layerID, proposals)
	require.NoError(t, err)
	assert.NotEqual(t, types.EmptyBlockID, block.ID())
	assert.Equal(t, layerID, block.LayerIndex)
	assert.Len(t, block.TxIDs, numTXs)

	// reorder the proposals
	ordered := proposals[numProposals/2 : numProposals]
	ordered = append(ordered, proposals[0:numProposals/2]...)
	require.NotEqual(t, proposals, ordered)
	block2, err := tg.GenerateBlock(context.TODO(), layerID, ordered)
	require.NoError(t, err)
	assert.Equal(t, block, block2)
	assert.Equal(t, block.ID(), block2.ID())
}

func Test_GenerateBlock_SameCoinbase(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	numTXs := 1000
	totalFees, txIDs, txs := createTransactions(t, numTXs)
	signer := signing.NewEdSigner()
	atx1, proposal1 := createProposalATXWithCoinbase(t, layerID, txIDs[0:500], signer)
	atx2, proposal2 := createProposalATXWithCoinbase(t, layerID, txIDs[400:], signer)
	require.Equal(t, atx1, atx2)

	tg.mockTXs.EXPECT().GetMeshTransactions(gomock.Any()).DoAndReturn(
		func(got []types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
			assert.ElementsMatch(t, got, txIDs)
			return txs, nil
		}).Times(1)
	tg.mockATXDB.EXPECT().GetAtxHeader(atx1.ID()).Return(atx1.ActivationTxHeader, nil).Times(2)
	block, err := tg.GenerateBlock(context.TODO(), layerID, []*types.Proposal{proposal1, proposal2})
	require.NoError(t, err)
	assert.NotEqual(t, types.EmptyBlockID, block.ID())

	assert.Equal(t, layerID, block.LayerIndex)
	assert.Len(t, block.TxIDs, numTXs)

	totalRewards := totalFees + testConfig().BaseReward
	unitReward := totalRewards / uint64(2)
	unitLayerReward := testConfig().BaseReward / uint64(2)
	assert.Len(t, block.Rewards, 2)
	for _, r := range block.Rewards {
		assert.Equal(t, unitReward, r.Amount)
		assert.Equal(t, unitLayerReward, r.LayerReward)
		assert.Equal(t, types.BytesToAddress(signer.PublicKey().Bytes()), r.Address)
		assert.Equal(t, signer.PublicKey().Bytes(), r.SmesherID[:])
	}

	// make sure the rewards remainder, tho ignored, is correct
	totalRemainder := totalRewards % uint64(2)
	assert.Equal(t, totalRewards, unitReward*uint64(2)+totalRemainder)
}

func Test_GenerateBlock_EmptyATXID(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	_, txIDs, txs := createTransactions(t, numTXs)
	atxs, proposals := createProposalsWithOverlappingTXs(t, layerID, numProposals, txIDs)
	// set the last proposal ID to be empty
	types.SortProposals(proposals)
	proposals[numProposals-1].AtxID = *types.EmptyATXID

	tg.mockTXs.EXPECT().GetMeshTransactions(gomock.Any()).DoAndReturn(
		func(got []types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
			assert.ElementsMatch(t, got, txIDs)
			return txs, nil
		}).Times(1)
	tg.mockATXDB.EXPECT().GetAtxHeader(gomock.Any()).DoAndReturn(
		func(id types.ATXID) (*types.ActivationTxHeader, error) {
			return atxs[id].ActivationTxHeader, nil
		}).Times(numProposals - 1)
	block, err := tg.GenerateBlock(context.TODO(), layerID, proposals)
	assert.ErrorIs(t, err, errInvalidATXID)
	assert.Nil(t, block)
}

func Test_GenerateBlock_ATXNotFound(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	_, txIDs, txs := createTransactions(t, numTXs)
	atxs, proposals := createProposalsWithOverlappingTXs(t, layerID, numProposals, txIDs)
	tg.mockTXs.EXPECT().GetMeshTransactions(gomock.Any()).DoAndReturn(
		func(got []types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
			assert.ElementsMatch(t, got, txIDs)
			return txs, nil
		}).Times(1)
	errUnknown := errors.New("unknown")
	tg.mockATXDB.EXPECT().GetAtxHeader(gomock.Any()).DoAndReturn(
		func(id types.ATXID) (*types.ActivationTxHeader, error) {
			if id == proposals[numProposals-1].AtxID {
				return nil, errUnknown
			}
			return atxs[id].ActivationTxHeader, nil
		}).Times(numProposals)
	block, err := tg.GenerateBlock(context.TODO(), layerID, proposals)
	assert.ErrorIs(t, err, errUnknown)
	assert.Nil(t, block)
}

func Test_GenerateBlock_TXNotFound(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	_, txIDs, txs := createTransactions(t, numTXs)
	_, proposals := createProposalsWithOverlappingTXs(t, layerID, numProposals, txIDs)
	tg.mockTXs.EXPECT().GetMeshTransactions(gomock.Any()).DoAndReturn(
		func(got []types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
			assert.ElementsMatch(t, got, txIDs)
			return txs[1:], map[types.TransactionID]struct{}{txs[0].ID(): {}}
		}).Times(1)
	block, err := tg.GenerateBlock(context.TODO(), layerID, proposals)
	assert.ErrorIs(t, err, errTXNotFound)
	assert.Nil(t, block)
}

func Test_GenerateBlock_MultipleEligibilities(t *testing.T) {
	tg := createTestGenerator(t)
	fee, ids, txs := createTransactions(t, 1000)
	tg.mockTXs.EXPECT().GetMeshTransactions(gomock.Any()).Return(txs, nil).Times(1)
	header := &types.ActivationTxHeader{}
	header.SetID(&types.ATXID{1, 1, 1})

	genProposal := func(proofs int) *types.Proposal {
		return &types.Proposal{
			InnerProposal: types.InnerProposal{
				Ballot: types.Ballot{
					InnerBallot: types.InnerBallot{
						EligibilityProofs: make([]types.VotingEligibilityProof, proofs),
						AtxID:             header.ID(),
					},
				},
				TxIDs: ids,
			},
		}
	}
	proposals := []*types.Proposal{
		genProposal(2), genProposal(1), genProposal(5),
	}
	tg.mockATXDB.EXPECT().GetAtxHeader(gomock.Any()).Return(header, nil).Times(len(proposals))
	total := uint64(0)
	for _, p := range proposals {
		total += uint64(len(p.EligibilityProofs))
	}

	block, err := tg.GenerateBlock(context.TODO(), types.NewLayerID(10), proposals)
	require.NoError(t, err)
	require.Len(t, block.Rewards, len(proposals))

	expected := (fee + tg.cfg.BaseReward) / total
	for i, p := range proposals {
		require.Equal(t, int(expected)*len(p.EligibilityProofs), int(block.Rewards[i].Amount))
	}
}
