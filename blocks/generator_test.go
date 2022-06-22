package blocks

import (
	"bytes"
	"context"
	"sort"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
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
	ctrl       *gomock.Controller
	mockMesh   *mocks.MockmeshProvider
	mockCState *mocks.MockconservativeState
}

func createTestGenerator(t *testing.T) *testGenerator {
	types.SetLayersPerEpoch(3)
	ctrl := gomock.NewController(t)
	tg := &testGenerator{
		mockMesh:   mocks.NewMockmeshProvider(ctrl),
		mockCState: mocks.NewMockconservativeState(ctrl),
	}
	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	tg.Generator = NewGenerator(cdb, tg.mockCState, WithGeneratorLogger(lg), WithConfig(testConfig()))
	return tg
}

func genTx(t testing.TB, signer *signing.EdSigner, dest types.Address, amount, nonce, price uint64) types.Transaction {
	t.Helper()
	raw := wallet.Spend(signer.PrivateKey(), dest, amount,
		sdk.WithNonce(types.Nonce{Counter: nonce}),
	)
	tx := types.Transaction{
		RawTx:    types.NewRawTx(raw),
		TxHeader: &types.TxHeader{},
	}
	tx.MaxGas = 100
	tx.MaxSpend = amount
	tx.GasPrice = price
	tx.Nonce = types.Nonce{Counter: nonce}
	tx.Principal = types.BytesToAddress(signer.PublicKey().Bytes())
	return tx
}

func createTransactions(t testing.TB, numOfTxs int) []types.TransactionID {
	t.Helper()
	txIDs := make([]types.TransactionID, 0, numOfTxs)
	for i := 0; i < numOfTxs; i++ {
		tx := genTx(t, signing.NewEdSigner(), types.HexToAddress("1"), 1, 10, 100)
		txIDs = append(txIDs, tx.ID)
	}
	return txIDs
}

func createATXs(t *testing.T, cdb *datastore.CachedDB, lid types.LayerID, numATXs int) ([]*signing.EdSigner, []*types.ActivationTx) {
	t.Helper()
	atxes := make([]*types.ActivationTx, 0, numATXs)
	signers := make([]*signing.EdSigner, 0, numATXs)
	for i := 0; i < numATXs; i++ {
		signer := signing.NewEdSigner()
		signers = append(signers, signer)
		address := types.BytesToAddress(signer.PublicKey().Bytes())
		nipostChallenge := types.NIPostChallenge{
			NodeID:     types.BytesToNodeID(signer.PublicKey().Bytes()),
			StartTick:  1,
			EndTick:    2,
			PubLayerID: lid,
		}
		atx := types.NewActivationTx(nipostChallenge, address, nil, numUint, nil)
		require.NoError(t, atxs.Add(cdb, atx, time.Now()))
		atxes = append(atxes, atx)
	}
	return signers, atxes
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

func Test_GenerateBlock(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs := createTransactions(t, numTXs)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	proposals := createProposals(t, layerID, signers, activeSet, txIDs)
	tg.mockCState.EXPECT().SelectBlockTXs(layerID, proposals).Return(txIDs, nil)
	block, err := tg.GenerateBlock(context.TODO(), layerID, proposals)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyBlockID, block.ID())

	require.Equal(t, layerID, block.LayerIndex)
	require.Len(t, block.TxIDs, numTXs)
	require.Len(t, block.Rewards, numProposals)
	// numUint is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1 eligibility
	// the expected weight for each eligibility is `numUnit` * 1/3
	expWeight := util.WeightFromInt64(numUint * 1 / 3)
	checkRewards(t, atxes, expWeight, block.Rewards)
}

func Test_GenerateBlock_EmptyProposals(t *testing.T) {
	tg := createTestGenerator(t)
	numProposals := 10
	lid := types.GetEffectiveGenesis().Add(20)
	signers, atxes := createATXs(t, tg.cdb, (lid.GetEpoch() - 1).FirstLayer(), numProposals)
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
	tg.mockCState.EXPECT().SelectBlockTXs(lid, proposals).Return(nil, nil)
	block, err := tg.GenerateBlock(context.TODO(), lid, proposals)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyBlockID, block.ID())

	require.Equal(t, lid, block.LayerIndex)
	require.Empty(t, block.TxIDs)
	require.Len(t, block.Rewards, numProposals)
	// numUint is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1 eligibility
	// the expected weight for each eligibility is `numUnit` * 1/3
	expWeight := util.WeightFromInt64(numUint * 1 / 3)
	checkRewards(t, atxes, expWeight, block.Rewards)
}

func Test_GenerateBlockStableBlockID(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs := createTransactions(t, numTXs)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	proposals := createProposals(t, layerID, signers, activeSet, txIDs)
	tg.mockCState.EXPECT().SelectBlockTXs(layerID, proposals).Return(txIDs, nil)
	block, err := tg.GenerateBlock(context.TODO(), layerID, proposals)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyBlockID, block.ID())
	require.Equal(t, layerID, block.LayerIndex)
	require.Len(t, block.TxIDs, numTXs)

	// reorder the proposals
	ordered := proposals[numProposals/2 : numProposals]
	ordered = append(ordered, proposals[0:numProposals/2]...)
	require.NotEqual(t, proposals, ordered)
	tg.mockCState.EXPECT().SelectBlockTXs(layerID, ordered).Return(txIDs, nil)
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
	txIDs := createTransactions(t, numTXs)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	epochData := &types.EpochData{
		ActiveSet: activeSet,
		Beacon:    types.RandomBeacon(),
	}
	atxID := activeSet[0]
	proposal1 := createProposal(t, epochData, layerID, atxID, signers[0], txIDs[0:500], 1)
	proposal2 := createProposal(t, epochData, layerID, atxID, signers[0], txIDs[400:], 1)
	proposals := []*types.Proposal{proposal1, proposal2}
	tg.mockCState.EXPECT().SelectBlockTXs(layerID, proposals).Return(txIDs, nil)
	block, err := tg.GenerateBlock(context.TODO(), layerID, proposals)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyBlockID, block.ID())

	require.Equal(t, layerID, block.LayerIndex)
	require.Len(t, block.TxIDs, numTXs)

	// numUint is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1 eligibility
	// the expected weight for each eligibility is `numUnit` * 1/3
	// since there are two proposals for the same coinbase, the final weight is `numUnit` * 1/3 * 2
	expWeight := util.WeightFromInt64(numUint * 1 / 3 * 2)
	checkRewards(t, atxes[0:1], expWeight, block.Rewards)
}

func Test_GenerateBlock_EmptyATXID(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs := createTransactions(t, numTXs)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	proposals := createProposals(t, layerID, signers, activeSet, txIDs)
	// set the last proposal ID to be empty
	types.SortProposals(proposals)
	proposals[numProposals-1].AtxID = *types.EmptyATXID

	tg.mockCState.EXPECT().SelectBlockTXs(layerID, proposals).Return(txIDs, nil)
	block, err := tg.GenerateBlock(context.TODO(), layerID, proposals)
	require.ErrorIs(t, err, errInvalidATXID)
	require.Nil(t, block)
}

func Test_GenerateBlock_MultipleEligibilities(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	ids := createTransactions(t, 1000)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	epochData := &types.EpochData{
		ActiveSet: activeSet,
		Beacon:    types.RandomBeacon(),
	}
	proposals := []*types.Proposal{
		createProposal(t, epochData, layerID, atxes[0].ID(), signers[0], ids, 2),
		createProposal(t, epochData, layerID, atxes[1].ID(), signers[1], ids, 1),
		createProposal(t, epochData, layerID, atxes[2].ID(), signers[2], ids, 5),
	}

	tg.mockCState.EXPECT().SelectBlockTXs(layerID, proposals).Return(ids, nil)
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
