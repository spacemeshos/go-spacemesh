package blocks

import (
	"bytes"
	"errors"
	"sort"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	maxFee = 2000
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
}

func createTestGenerator(t *testing.T) *testGenerator {
	ctrl := gomock.NewController(t)
	tg := &testGenerator{
		ctrl:      ctrl,
		mockMesh:  mocks.NewMockmeshProvider(ctrl),
		mockATXDB: mocks.NewMockatxProvider(ctrl),
	}
	tg.Generator = NewGenerator(tg.mockATXDB, tg.mockMesh, WithGeneratorLogger(logtest.New(t)), WithConfig(testConfig()))
	return tg
}

func createTransactions(t testing.TB, numOfTxs int) (uint64, []types.TransactionID, []*types.Transaction) {
	t.Helper()
	var totalFee uint64
	txs := make([]*types.Transaction, 0, numOfTxs)
	txIDs := make([]types.TransactionID, 0, numOfTxs)
	for i := 0; i < numOfTxs; i++ {
		fee := uint64(rand.Intn(maxFee))
		tx, err := types.GenerateCallTransaction(signing.NewEdSigner(), types.HexToAddress("1"), 1, 10, 100, fee)
		require.NoError(t, err)
		totalFee += fee
		txs = append(txs, tx)
		txIDs = append(txIDs, tx.ID())
	}
	return totalFee, txIDs, txs
}

type smesher struct {
	addr   types.Address
	nodeID types.NodeID
}

func createProposalsWithOverlappingTXs(t *testing.T, layerID types.LayerID, numProposals int, txIDs []types.TransactionID) (
	[]*smesher, map[types.ATXID]*types.ActivationTx, []*types.Proposal) {
	t.Helper()
	require.Zero(t, len(txIDs)%numProposals)
	pieSize := len(txIDs) / numProposals
	smeshers := make([]*smesher, 0, numProposals)
	atxs := make(map[types.ATXID]*types.ActivationTx)
	proposals := make([]*types.Proposal, 0, numProposals)
	for i := 0; i < numProposals; i++ {
		from := i * pieSize
		to := from + (2 * pieSize) // every adjacent proposal will overlap pieSize of TXs
		if to > len(txIDs) {
			to = len(txIDs)
		}
		thisPie := txIDs[from:to]
		smesher, atx, p := createProposalWithATX(t, layerID, thisPie)
		smeshers = append(smeshers, smesher)
		atxs[atx.ID()] = atx
		proposals = append(proposals, p)
	}
	return smeshers, atxs, proposals
}

func createProposalWithATX(t *testing.T, layerID types.LayerID, txIDs []types.TransactionID) (*smesher, *types.ActivationTx, *types.Proposal) {
	t.Helper()
	signer := signing.NewEdSigner()
	address := types.BytesToAddress(signer.PublicKey().Bytes())
	nipostChallenge := types.NIPostChallenge{
		NodeID: types.NodeID{
			Key:          signer.PublicKey().String(),
			VRFPublicKey: signer.PublicKey().Bytes(),
		},
		StartTick:  1,
		EndTick:    2,
		PubLayerID: layerID,
	}
	atx := types.NewActivationTx(nipostChallenge, address, nil, uint(rand.Uint32()), nil)

	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot: types.InnerBallot{
					AtxID:      atx.ID(),
					LayerIndex: layerID,
				},
			},
			TxIDs: txIDs,
		},
	}
	p.Ballot.Signature = signer.Sign(p.Ballot.Bytes())
	p.Signature = signer.Sign(p.Bytes())
	require.NoError(t, p.Initialize())
	return &smesher{address, atx.NodeID}, atx, p
}

func Test_GenerateBlock(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	totalFees, txIDs, txs := createTransactions(t, numTXs)
	smeshers, atxs, proposals := createProposalsWithOverlappingTXs(t, layerID, numProposals, txIDs)
	tg.mockMesh.EXPECT().GetTransactions(gomock.Any()).DoAndReturn(
		func(got []types.TransactionID) ([]*types.Transaction, map[types.TransactionID]struct{}) {
			assert.ElementsMatch(t, got, txIDs)
			return txs, nil
		}).Times(1)
	tg.mockATXDB.EXPECT().GetAtxHeader(gomock.Any()).DoAndReturn(
		func(id types.ATXID) (*types.ActivationTxHeader, error) {
			return atxs[id].ActivationTxHeader, nil
		}).Times(numProposals)
	block, err := tg.GenerateBlock(layerID, proposals)
	require.NoError(t, err)

	assert.Equal(t, layerID, block.LayerIndex)
	assert.Len(t, block.TxIDs, numTXs)

	totalRewards := totalFees + testConfig().BaseReward
	unitReward := totalRewards / uint64(numProposals)
	unitLayerReward := testConfig().BaseReward / uint64(numProposals)
	// rewards is sorted by coinbase address
	sort.Slice(smeshers[:], func(i, j int) bool {
		return bytes.Compare(smeshers[i].addr.Bytes(), smeshers[j].addr.Bytes()) < 0
	})
	for i, r := range block.Rewards {
		assert.Equal(t, unitReward, r.Amount)
		assert.Equal(t, unitLayerReward, r.LayerReward)
		assert.Equal(t, smeshers[i].addr, r.Address)
		assert.Equal(t, smeshers[i].nodeID, r.SmesherID)
	}

	// make sure the rewards remainder, tho ignored, is correct
	totalRemainder := totalRewards % uint64(numProposals)
	assert.Equal(t, totalRewards, unitReward*uint64(numProposals)+totalRemainder)
}

func Test_GenerateBlock_EmptyATXID(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	_, txIDs, txs := createTransactions(t, numTXs)
	_, atxs, proposals := createProposalsWithOverlappingTXs(t, layerID, numProposals, txIDs)
	// set the last proposal ID to be empty
	proposals[numProposals-1].AtxID = *types.EmptyATXID

	tg.mockMesh.EXPECT().GetTransactions(gomock.Any()).DoAndReturn(
		func(got []types.TransactionID) ([]*types.Transaction, map[types.TransactionID]struct{}) {
			assert.ElementsMatch(t, got, txIDs)
			return txs, nil
		}).Times(1)
	tg.mockATXDB.EXPECT().GetAtxHeader(gomock.Any()).DoAndReturn(
		func(id types.ATXID) (*types.ActivationTxHeader, error) {
			return atxs[id].ActivationTxHeader, nil
		}).Times(numProposals - 1)
	block, err := tg.GenerateBlock(layerID, proposals)
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
	_, atxs, proposals := createProposalsWithOverlappingTXs(t, layerID, numProposals, txIDs)
	tg.mockMesh.EXPECT().GetTransactions(gomock.Any()).DoAndReturn(
		func(got []types.TransactionID) ([]*types.Transaction, map[types.TransactionID]struct{}) {
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
	block, err := tg.GenerateBlock(layerID, proposals)
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
	_, _, proposals := createProposalsWithOverlappingTXs(t, layerID, numProposals, txIDs)
	tg.mockMesh.EXPECT().GetTransactions(gomock.Any()).DoAndReturn(
		func(got []types.TransactionID) ([]*types.Transaction, map[types.TransactionID]struct{}) {
			assert.ElementsMatch(t, got, txIDs)
			return txs[1:], map[types.TransactionID]struct{}{txs[0].ID(): {}}
		}).Times(1)
	block, err := tg.GenerateBlock(layerID, proposals)
	assert.ErrorIs(t, err, errTXNotFound)
	assert.Nil(t, block)
}
