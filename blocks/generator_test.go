package blocks

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
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
	mockMesh   *mocks.MockmeshProvider
	mockCState *mocks.MockconservativeState
	mockFetch  *smocks.MockProposalFetcher
	mockCert   *mocks.Mockcertifier
}

func createTestGenerator(t *testing.T) *testGenerator {
	types.SetLayersPerEpoch(3)
	ctrl := gomock.NewController(t)
	tg := &testGenerator{
		mockMesh:   mocks.NewMockmeshProvider(ctrl),
		mockCState: mocks.NewMockconservativeState(ctrl),
		mockFetch:  smocks.NewMockProposalFetcher(ctrl),
		mockCert:   mocks.NewMockcertifier(ctrl),
	}
	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	tg.Generator = NewGenerator(cdb, tg.mockCState, tg.mockMesh, tg.mockFetch, tg.mockCert, WithGeneratorLogger(lg), WithConfig(testConfig()))
	return tg
}

func genTx(t testing.TB, signer *signing.EdSigner, dest types.Address, amount, nonce, price uint64) types.Transaction {
	t.Helper()
	raw := wallet.Spend(signer.PrivateKey(), dest, amount, nonce)
	tx := types.Transaction{
		RawTx:    types.NewRawTx(raw),
		TxHeader: &types.TxHeader{},
	}
	tx.MaxGas = 100
	tx.MaxSpend = amount
	tx.GasPrice = price
	tx.Nonce = nonce
	tx.Principal = types.GenerateAddress(signer.PublicKey().Bytes())
	return tx
}

func createTransactions(t testing.TB, numOfTxs int) []types.TransactionID {
	t.Helper()
	txIDs := make([]types.TransactionID, 0, numOfTxs)
	for i := 0; i < numOfTxs; i++ {
		addr := types.GenerateAddress([]byte("1"))
		tx := genTx(t, signing.NewEdSigner(), addr, 1, 10, 100)
		txIDs = append(txIDs, tx.ID)
	}
	return txIDs
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
			numUint,
			nil,
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

func createProposals(t *testing.T, db sql.Executor, layerID types.LayerID, signers []*signing.EdSigner, activeSet []types.ATXID, txIDs []types.TransactionID) []*types.Proposal {
	t.Helper()
	numProposals := len(signers)
	// make sure proposals have overlapping transactions
	require.Zero(t, len(txIDs)%numProposals)
	pieSize := len(txIDs) / numProposals
	plist := make([]*types.Proposal, 0, numProposals)
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
		plist = append(plist, p)
		require.NoError(t, proposals.Add(db, p))
		require.NoError(t, ballots.Add(db, &p.Ballot))
	}
	return plist
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

func checkRewards(t *testing.T, atxs []*types.ActivationTx, expWeightPer *big.Rat, rewards []types.AnyReward) {
	t.Helper()
	sort.Slice(atxs, func(i, j int) bool {
		return bytes.Compare(atxs[i].Coinbase.Bytes(), atxs[j].Coinbase.Bytes()) < 0
	})
	for i, r := range rewards {
		require.Equal(t, atxs[i].Coinbase, r.Coinbase)
		got := r.Weight.ToBigRat()
		require.Equal(t, expWeightPer, got)
	}
}

func Test_StartStop(t *testing.T) {
	tg := createTestGenerator(t)
	tg.Start()
	tg.Start() // start for the second time is ok.
	tg.Stop()
}

func Test_processHareOutput(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs := createTransactions(t, numTXs)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	plist := createProposals(t, tg.cdb, layerID, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	var block *types.Block
	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids).Return(nil)
	tg.mockCState.EXPECT().SelectBlockTXs(layerID, plist).Return(txIDs, nil)
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, b *types.Block) error {
			block = b
			require.NotEqual(t, types.EmptyBlockID, block.ID())
			require.Equal(t, layerID, block.LayerIndex)
			require.Len(t, block.TxIDs, numTXs)
			require.Len(t, block.Rewards, numProposals)
			// numUint is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1 eligibility
			// the expected weight for each eligibility is `numUnit` * 1/3
			expWeight := new(big.Rat).SetInt64(numUint * 1 / 3)
			checkRewards(t, atxes, expWeight, block.Rewards)
			return nil
		})
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, got types.BlockID) error {
			require.Equal(t, block.ID(), got)
			return nil
		})
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ log.Log, _ types.LayerID, got types.BlockID) error {
			require.Equal(t, block.ID(), got)
			return nil
		})
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, got types.BlockID) error {
			require.Equal(t, block.ID(), got)
			return nil
		})
	require.NoError(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.TODO(), Layer: layerID, Proposals: pids}))
}

func Test_processHareOutput_EmptyOutput(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, types.EmptyBlockID).Return(nil)
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), layerID, types.EmptyBlockID).Return(nil)
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, types.EmptyBlockID).Return(nil)
	require.NoError(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.TODO(), Layer: layerID}))
}

func Test_processHareOutput_ProcessFailed(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs := createTransactions(t, numTXs)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	plist := createProposals(t, tg.cdb, layerID, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	var block *types.Block
	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids).Return(nil)
	tg.mockCState.EXPECT().SelectBlockTXs(layerID, plist).Return(txIDs, nil)
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, b *types.Block) error {
			block = b
			require.NotEqual(t, types.EmptyBlockID, block.ID())
			require.Equal(t, layerID, block.LayerIndex)
			require.Len(t, block.TxIDs, numTXs)
			require.Len(t, block.Rewards, numProposals)
			// numUint is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1 eligibility
			// the expected weight for each eligibility is `numUnit` * 1/3
			expWeight := new(big.Rat).SetInt64(numUint * 1 / 3)
			checkRewards(t, atxes, expWeight, block.Rewards)
			return nil
		})
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, got types.BlockID) error {
			require.Equal(t, block.ID(), got)
			return nil
		})
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ log.Log, _ types.LayerID, got types.BlockID) error {
			require.Equal(t, block.ID(), got)
			return nil
		})
	errUnknown := errors.New("unknown")
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, gomock.Any()).Return(errUnknown)
	require.ErrorIs(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.TODO(), Layer: layerID, Proposals: pids}), errUnknown)
}

func Test_processHareOutput_AddBlockFailed(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs := createTransactions(t, numTXs)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	plist := createProposals(t, tg.cdb, layerID, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids).Return(nil)
	tg.mockCState.EXPECT().SelectBlockTXs(layerID, plist).Return(txIDs, nil)
	errUnknown := errors.New("unknown")
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any()).Return(errUnknown)
	require.ErrorIs(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.TODO(), Layer: layerID, Proposals: pids}), errUnknown)
}

func Test_processHareOutput_TxPoolFailed(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs := createTransactions(t, numTXs)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	plist := createProposals(t, tg.cdb, layerID, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids).Return(nil)
	errUnknown := errors.New("unknown")
	tg.mockCState.EXPECT().SelectBlockTXs(layerID, plist).Return(nil, errUnknown)
	require.ErrorIs(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.TODO(), Layer: layerID, Proposals: pids}), errUnknown)
}

func Test_processHareOutput_FetchFailed(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs := createTransactions(t, numTXs)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	plist := createProposals(t, tg.cdb, layerID, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	errUnknown := errors.New("unknown")
	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids).Return(errUnknown)
	require.ErrorIs(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.TODO(), Layer: layerID, Proposals: pids}), errUnknown)
}

func Test_generateBlock_UnequalHeight(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	numProposals := 10
	rng := rand.New(rand.NewSource(10101))
	max := uint64(0)
	signers, atxes := createModifiedATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals, func(atx *types.ActivationTx) (*types.VerifiedActivationTx, error) {
		n := rng.Uint64()
		if n > max {
			max = n
		}
		return atx.Verify(n, 1)
	})
	activeSet := types.ToATXIDs(atxes)
	pList := createProposals(t, tg.cdb, layerID, signers, activeSet, nil)
	tg.mockCState.EXPECT().SelectBlockTXs(layerID, pList).Return(nil, nil)
	block, err := tg.generateBlock(context.TODO(), layerID, pList)
	require.NoError(t, err)
	require.Equal(t, max, block.TickHeight)
}

func Test_generateBlock_EmptyProposals(t *testing.T) {
	tg := createTestGenerator(t)
	numProposals := 10
	lid := types.GetEffectiveGenesis().Add(20)
	signers, atxes := createATXs(t, tg.cdb, (lid.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	epochData := &types.EpochData{
		ActiveSet: activeSet,
		Beacon:    types.RandomBeacon(),
	}
	plist := make([]*types.Proposal, 0, numProposals)
	for i := 0; i < numProposals; i++ {
		p := createProposal(t, epochData, lid, activeSet[i], signers[i], nil, 1)
		plist = append(plist, p)
	}
	tg.mockCState.EXPECT().SelectBlockTXs(lid, plist).Return(nil, nil)
	block, err := tg.generateBlock(context.TODO(), lid, plist)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyBlockID, block.ID())

	require.Equal(t, lid, block.LayerIndex)
	require.Empty(t, block.TxIDs)
	require.Len(t, block.Rewards, numProposals)
	// numUint is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1 eligibility
	// the expected weight for each eligibility is `numUnit` * 1/3
	expWeight := new(big.Rat).SetInt64(numUint * 1 / 3)
	checkRewards(t, atxes, expWeight, block.Rewards)
}

func Test_generateBlock_StableBlockID(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs := createTransactions(t, numTXs)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	plist := createProposals(t, tg.cdb, layerID, signers, activeSet, txIDs)
	tg.mockCState.EXPECT().SelectBlockTXs(layerID, plist).Return(txIDs, nil)
	block, err := tg.generateBlock(context.TODO(), layerID, plist)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyBlockID, block.ID())
	require.Equal(t, layerID, block.LayerIndex)
	require.Len(t, block.TxIDs, numTXs)

	// reorder the proposals
	ordered := plist[numProposals/2 : numProposals]
	ordered = append(ordered, plist[0:numProposals/2]...)
	require.NotEqual(t, plist, ordered)
	tg.mockCState.EXPECT().SelectBlockTXs(layerID, ordered).Return(txIDs, nil)
	block2, err := tg.generateBlock(context.TODO(), layerID, ordered)
	require.NoError(t, err)
	require.Equal(t, block, block2)
	require.Equal(t, block.ID(), block2.ID())
}

func Test_generateBlock_SameCoinbase(t *testing.T) {
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
	plist := []*types.Proposal{proposal1, proposal2}
	tg.mockCState.EXPECT().SelectBlockTXs(layerID, plist).Return(txIDs, nil)
	block, err := tg.generateBlock(context.TODO(), layerID, plist)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyBlockID, block.ID())

	require.Equal(t, layerID, block.LayerIndex)
	require.Len(t, block.TxIDs, numTXs)

	// numUint is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1 eligibility
	// the expected weight for each eligibility is `numUnit` * 1/3
	// since there are two proposals for the same coinbase, the final weight is `numUnit` * 1/3 * 2
	expWeight := new(big.Rat).SetInt64(numUint * 1 / 3 * 2)
	checkRewards(t, atxes[0:1], expWeight, block.Rewards)
}

func Test_generateBlock_EmptyATXID(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs := createTransactions(t, numTXs)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	plist := createProposals(t, tg.cdb, layerID, signers, activeSet, txIDs)
	// set the last proposal ID to be empty
	types.SortProposals(plist)
	plist[numProposals-1].AtxID = *types.EmptyATXID

	tg.mockCState.EXPECT().SelectBlockTXs(layerID, plist).Return(txIDs, nil)
	block, err := tg.generateBlock(context.TODO(), layerID, plist)
	require.ErrorIs(t, err, errInvalidATXID)
	require.Nil(t, block)
}

func Test_generateBlock_MultipleEligibilities(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	ids := createTransactions(t, 1000)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	epochData := &types.EpochData{
		ActiveSet: activeSet,
		Beacon:    types.RandomBeacon(),
	}
	plist := []*types.Proposal{
		createProposal(t, epochData, layerID, atxes[0].ID(), signers[0], ids, 2),
		createProposal(t, epochData, layerID, atxes[1].ID(), signers[1], ids, 1),
		createProposal(t, epochData, layerID, atxes[2].ID(), signers[2], ids, 5),
	}

	tg.mockCState.EXPECT().SelectBlockTXs(layerID, plist).Return(ids, nil)
	block, err := tg.generateBlock(context.TODO(), layerID, plist)
	require.NoError(t, err)
	require.Len(t, block.Rewards, len(plist))
	sort.Slice(plist, func(i, j int) bool {
		cbi := types.GenerateAddress(plist[i].SmesherID().Bytes())
		cbj := types.GenerateAddress(plist[j].SmesherID().Bytes())
		return bytes.Compare(cbi.Bytes(), cbj.Bytes()) < 0
	})
	totalWeight := new(big.Rat)
	for i, r := range block.Rewards {
		require.Equal(t, types.GenerateAddress(plist[i].SmesherID().Bytes()), r.Coinbase)
		got := r.Weight.ToBigRat()
		// numUint is the ATX weight. eligible slots per epoch is 3 for each atx
		// the expected weight for each eligibility is `numUnit` * 1/3
		require.Equal(t, new(big.Rat).SetInt64(numUint*1/3*int64(len(plist[i].EligibilityProofs))), got)
		totalWeight.Add(totalWeight, got)
	}
	require.Equal(t, new(big.Rat).SetInt64(numUint*8*1/3), totalWeight)
}
