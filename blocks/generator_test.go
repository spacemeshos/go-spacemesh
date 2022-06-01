package blocks

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/transaction"
)

const (
	numUint = 12
)

func testConfig() Config {
	return Config{
		LayerSize:      10,
		LayersPerEpoch: 3,
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

func createTransactions(t testing.TB, numOfTxs int) ([]types.TransactionID, []*types.MeshTransaction) {
	t.Helper()
	txs := make([]*types.MeshTransaction, 0, numOfTxs)
	txIDs := make([]types.TransactionID, 0, numOfTxs)
	for i := 0; i < numOfTxs; i++ {
		tx, err := transaction.GenerateCallTransaction(signing.NewEdSigner(), types.HexToAddress("1"), 1, 10, 100, 3)
		require.NoError(t, err)
		txs = append(txs, &types.MeshTransaction{Transaction: *tx})
		txIDs = append(txIDs, tx.ID())
	}
	return txIDs, txs
}

func createATXs(t *testing.T, numATXs int) ([]*signing.EdSigner, []*types.ActivationTx, map[types.ATXID]*types.ActivationTx) {
	t.Helper()
	atxs := make([]*types.ActivationTx, 0, numATXs)
	atxByID := make(map[types.ATXID]*types.ActivationTx)
	signers := make([]*signing.EdSigner, 0, numATXs)
	for i := 0; i < numATXs; i++ {
		signer := signing.NewEdSigner()
		signers = append(signers, signer)
		address := types.BytesToAddress(signer.PublicKey().Bytes())
		nipostChallenge := types.NIPostChallenge{
			NodeID:    types.BytesToNodeID(signer.PublicKey().Bytes()),
			StartTick: 1,
			EndTick:   2,
		}
		atx := types.NewActivationTx(nipostChallenge, address, nil, numUint, nil)
		atxs = append(atxs, atx)
		atxByID[atx.ID()] = atx
	}
	return signers, atxs, atxByID
}

func createProposals(t *testing.T, layerID types.LayerID, signers []*signing.EdSigner, activeSet []types.ATXID, txIDs []types.TransactionID) []*types.Proposal {
	t.Helper()
	numProposals := len(signers)
	// make sure proposals have overlapping transactions
	require.Zero(t, len(txIDs)%numProposals)
	pieSize := len(txIDs) / numProposals
	proposals := make([]*types.Proposal, 0, numProposals)
	epochData := &types.EpochData{
		ActiveSet: activeSet,
		Beacon:    types.RandomBeacon(),
	}
	for i := 0; i < numProposals; i++ {
		from := i * pieSize
		to := from + (2 * pieSize) // every adjacent proposal will overlap pieSize of TXs
		if to > len(txIDs) {
			to = len(txIDs)
		}
		p := createProposal(t, epochData, layerID, activeSet[i], signers[i], txIDs[from:to], 1)
		proposals = append(proposals, p)
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
	p.Ballot.Signature = signer.Sign(p.Ballot.Bytes())
	p.Signature = signer.Sign(p.Bytes())
	require.NoError(t, p.Initialize())
	return p
}

func Test_GenerateBlock(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs, txs := createTransactions(t, numTXs)
	signers, atxs, atxByID := createATXs(t, numProposals)
	activeSet := types.ToATXIDs(atxs)
	proposals := createProposals(t, layerID, signers, activeSet, txIDs)
	tg.mockTXs.EXPECT().GetMeshTransactions(gomock.Any()).DoAndReturn(
		func(got []types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
			require.ElementsMatch(t, got, txIDs)
			return txs, nil
		}).Times(1)
	tg.mockATXDB.EXPECT().GetAtxHeader(gomock.Any()).DoAndReturn(
		func(id types.ATXID) (*types.ActivationTxHeader, error) {
			return atxByID[id].ActivationTxHeader, nil
		}).AnyTimes()
	block, err := tg.GenerateBlock(context.TODO(), layerID, proposals)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyBlockID, block.ID())

	require.Equal(t, layerID, block.LayerIndex)
	require.Len(t, block.TxIDs, numTXs)
	require.Len(t, block.Rewards, numProposals)
	sort.Slice(proposals, func(i, j int) bool {
		cbi := types.BytesToAddress(proposals[i].SmesherID().Bytes())
		cbj := types.BytesToAddress(proposals[j].SmesherID().Bytes())
		return bytes.Compare(cbi.Bytes(), cbj.Bytes()) < 0
	})
	for i, r := range block.Rewards {
		require.Equal(t, types.BytesToAddress(proposals[i].SmesherID().Bytes()), r.Coinbase)
		got := util.WeightFromNumDenom(r.Weight.Num, r.Weight.Denom)
		// numUint is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1 eligibility
		// the expected weight for each eligibility is `numUnit` * 1/3
		require.Equal(t, util.WeightFromInt64(numUint*1/3), got)
	}
}

func Test_GenerateBlockStableBlockID(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs, txs := createTransactions(t, numTXs)
	signers, atxs, atxByID := createATXs(t, numProposals)
	activeSet := types.ToATXIDs(atxs)
	proposals := createProposals(t, layerID, signers, activeSet, txIDs)
	tg.mockTXs.EXPECT().GetMeshTransactions(gomock.Any()).DoAndReturn(
		func(got []types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
			require.ElementsMatch(t, got, txIDs)
			return txs, nil
		}).Times(2)
	tg.mockATXDB.EXPECT().GetAtxHeader(gomock.Any()).DoAndReturn(
		func(id types.ATXID) (*types.ActivationTxHeader, error) {
			return atxByID[id].ActivationTxHeader, nil
		}).AnyTimes()
	block, err := tg.GenerateBlock(context.TODO(), layerID, proposals)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyBlockID, block.ID())
	require.Equal(t, layerID, block.LayerIndex)
	require.Len(t, block.TxIDs, numTXs)

	// reorder the proposals
	ordered := proposals[numProposals/2 : numProposals]
	ordered = append(ordered, proposals[0:numProposals/2]...)
	require.NotEqual(t, proposals, ordered)
	block2, err := tg.GenerateBlock(context.TODO(), layerID, ordered)
	require.NoError(t, err)
	require.Equal(t, block, block2)
	require.Equal(t, block.ID(), block2.ID())
}

func Test_GenerateBlock_SameCoinbase(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	numTXs := 1000
	numProposals := 10
	txIDs, txs := createTransactions(t, numTXs)
	signers, atxs, atxByID := createATXs(t, numProposals)
	activeSet := types.ToATXIDs(atxs)
	epochData := &types.EpochData{
		ActiveSet: activeSet,
		Beacon:    types.RandomBeacon(),
	}
	atxID := activeSet[0]
	proposal1 := createProposal(t, epochData, layerID, atxID, signers[0], txIDs[0:500], 1)
	proposal2 := createProposal(t, epochData, layerID, atxID, signers[0], txIDs[400:], 1)

	tg.mockTXs.EXPECT().GetMeshTransactions(gomock.Any()).DoAndReturn(
		func(got []types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
			require.ElementsMatch(t, got, txIDs)
			return txs, nil
		}).Times(1)
	tg.mockATXDB.EXPECT().GetAtxHeader(gomock.Any()).DoAndReturn(
		func(id types.ATXID) (*types.ActivationTxHeader, error) {
			return atxByID[id].ActivationTxHeader, nil
		}).AnyTimes()
	block, err := tg.GenerateBlock(context.TODO(), layerID, []*types.Proposal{proposal1, proposal2})
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyBlockID, block.ID())

	require.Equal(t, layerID, block.LayerIndex)
	require.Len(t, block.TxIDs, numTXs)
	require.Len(t, block.Rewards, 1)
	r := block.Rewards[0]
	require.Equal(t, atxs[0].Coinbase, r.Coinbase)
	got := util.WeightFromNumDenom(r.Weight.Num, r.Weight.Denom)
	// numUint is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1 eligibility
	// the expected weight for each eligibility is `numUnit` * 1/3
	// since there are two proposals for the same coinbase, the final weight is `numUnit` * 1/3 * 2
	require.Equal(t, util.WeightFromInt64(numUint*1/3*2), got)
}

func Test_GenerateBlock_EmptyATXID(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs, txs := createTransactions(t, numTXs)
	signers, atxs, atxByID := createATXs(t, numProposals)
	activeSet := types.ToATXIDs(atxs)
	proposals := createProposals(t, layerID, signers, activeSet, txIDs)
	// set the last proposal ID to be empty
	types.SortProposals(proposals)
	proposals[numProposals-1].AtxID = *types.EmptyATXID

	tg.mockTXs.EXPECT().GetMeshTransactions(gomock.Any()).DoAndReturn(
		func(got []types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
			require.ElementsMatch(t, got, txIDs)
			return txs, nil
		}).Times(1)
	tg.mockATXDB.EXPECT().GetAtxHeader(gomock.Any()).DoAndReturn(
		func(id types.ATXID) (*types.ActivationTxHeader, error) {
			return atxByID[id].ActivationTxHeader, nil
		}).AnyTimes()
	block, err := tg.GenerateBlock(context.TODO(), layerID, proposals)
	require.ErrorIs(t, err, errInvalidATXID)
	require.Nil(t, block)
}

func Test_GenerateBlock_ATXNotFound(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs, txs := createTransactions(t, numTXs)
	signers, atxs, atxByID := createATXs(t, numProposals)
	activeSet := types.ToATXIDs(atxs)
	proposals := createProposals(t, layerID, signers, activeSet, txIDs)
	tg.mockTXs.EXPECT().GetMeshTransactions(gomock.Any()).DoAndReturn(
		func(got []types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
			require.ElementsMatch(t, got, txIDs)
			return txs, nil
		}).Times(1)
	errUnknown := errors.New("unknown")
	tg.mockATXDB.EXPECT().GetAtxHeader(gomock.Any()).DoAndReturn(
		func(id types.ATXID) (*types.ActivationTxHeader, error) {
			if id == proposals[numProposals-1].AtxID {
				return nil, errUnknown
			}
			return atxByID[id].ActivationTxHeader, nil
		}).AnyTimes()
	block, err := tg.GenerateBlock(context.TODO(), layerID, proposals)
	require.ErrorIs(t, err, errUnknown)
	require.Nil(t, block)
}

func Test_GenerateBlock_TXNotFound(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs, txs := createTransactions(t, numTXs)
	signers, atxs, _ := createATXs(t, numProposals)
	activeSet := types.ToATXIDs(atxs)
	proposals := createProposals(t, layerID, signers, activeSet, txIDs)
	tg.mockTXs.EXPECT().GetMeshTransactions(gomock.Any()).DoAndReturn(
		func(got []types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
			require.ElementsMatch(t, got, txIDs)
			return txs[1:], map[types.TransactionID]struct{}{txs[0].ID(): {}}
		}).Times(1)
	block, err := tg.GenerateBlock(context.TODO(), layerID, proposals)
	require.ErrorIs(t, err, errTXNotFound)
	require.Nil(t, block)
}

func Test_GenerateBlock_MultipleEligibilities(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	ids, txs := createTransactions(t, 1000)
	signers, atxs, atxByID := createATXs(t, 10)
	activeSet := types.ToATXIDs(atxs)
	epochData := &types.EpochData{
		ActiveSet: activeSet,
		Beacon:    types.RandomBeacon(),
	}
	proposals := []*types.Proposal{
		createProposal(t, epochData, layerID, atxs[0].ID(), signers[0], ids, 2),
		createProposal(t, epochData, layerID, atxs[1].ID(), signers[1], ids, 1),
		createProposal(t, epochData, layerID, atxs[2].ID(), signers[2], ids, 5),
	}

	tg.mockTXs.EXPECT().GetMeshTransactions(gomock.Any()).Return(txs, nil).Times(1)
	tg.mockATXDB.EXPECT().GetAtxHeader(gomock.Any()).DoAndReturn(
		func(id types.ATXID) (*types.ActivationTxHeader, error) {
			return atxByID[id].ActivationTxHeader, nil
		}).AnyTimes()

	block, err := tg.GenerateBlock(context.TODO(), layerID, proposals)
	require.NoError(t, err)
	require.Len(t, block.Rewards, len(proposals))
	sort.Slice(proposals, func(i, j int) bool {
		cbi := types.BytesToAddress(proposals[i].SmesherID().Bytes())
		cbj := types.BytesToAddress(proposals[j].SmesherID().Bytes())
		return bytes.Compare(cbi.Bytes(), cbj.Bytes()) < 0
	})
	totalWeight := util.WeightFromUint64(0)
	for i, r := range block.Rewards {
		require.Equal(t, types.BytesToAddress(proposals[i].SmesherID().Bytes()), r.Coinbase)
		got := util.WeightFromNumDenom(r.Weight.Num, r.Weight.Denom)
		// numUint is the ATX weight. eligible slots per epoch is 3 for each atx
		// the expected weight for each eligibility is `numUnit` * 1/3
		require.Equal(t, util.WeightFromInt64(numUint*1/3*int64(len(proposals[i].EligibilityProofs))), got)
		totalWeight.Add(got)
	}
	require.Equal(t, util.WeightFromInt64(numUint*8*1/3), totalWeight)
}
