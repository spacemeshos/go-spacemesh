package blocks

import (
	"bytes"
	"context"
	"errors"
	"math"
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
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	numUnit        = 12
	defaultGas     = 100
	baseTickHeight = 3
)

func testConfig() Config {
	return Config{
		LayerSize:          10,
		LayersPerEpoch:     3,
		GenBlockInterval:   10 * time.Millisecond,
		BlockGasLimit:      math.MaxUint64,
		OptFilterThreshold: 90,
	}
}

type testGenerator struct {
	*Generator
	mockMesh   *mocks.MockmeshProvider
	mockExec   *mocks.Mockexecutor
	mockFetch  *smocks.MockProposalFetcher
	mockCert   *mocks.Mockcertifier
	mockPatrol *mocks.MocklayerPatrol
}

func createTestGenerator(t *testing.T) *testGenerator {
	types.SetLayersPerEpoch(3)
	ctrl := gomock.NewController(t)
	tg := &testGenerator{
		mockMesh:   mocks.NewMockmeshProvider(ctrl),
		mockExec:   mocks.NewMockexecutor(ctrl),
		mockFetch:  smocks.NewMockProposalFetcher(ctrl),
		mockCert:   mocks.NewMockcertifier(ctrl),
		mockPatrol: mocks.NewMocklayerPatrol(ctrl),
	}
	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	tg.Generator = NewGenerator(cdb, tg.mockExec, tg.mockMesh, tg.mockFetch, tg.mockCert, tg.mockPatrol,
		WithGeneratorLogger(lg),
		WithHareOutputChan(make(chan hare.LayerOutput, 100)),
		WithConfig(testConfig()))
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

func createTransactions(tb testing.TB, numOfTxs int) []types.TransactionID {
	return createAndSaveTxs(tb, numOfTxs, nil)
}

func createAndSaveTxs(tb testing.TB, numOfTxs int, db sql.Executor) []types.TransactionID {
	tb.Helper()
	txIDs := make([]types.TransactionID, 0, numOfTxs)
	for i := 0; i < numOfTxs; i++ {
		addr := types.GenerateAddress([]byte("1"))
		signer, err := signing.NewEdSigner()
		require.NoError(tb, err)
		tx := genTx(tb, signer, addr, 1, 10, 100)
		if db != nil {
			require.NoError(tb, transactions.Add(db, &tx, time.Now()))
		}
		txIDs = append(txIDs, tx.ID)
	}
	return txIDs
}

func createATXs(t *testing.T, cdb *datastore.CachedDB, lid types.LayerID, numATXs int) ([]*signing.EdSigner, []*types.ActivationTx) {
	return createModifiedATXs(t, cdb, lid, numATXs, func(atx *types.ActivationTx) (*types.VerifiedActivationTx, error) {
		return atx.Verify(baseTickHeight, 1)
	})
}

func createModifiedATXs(tb testing.TB, cdb *datastore.CachedDB, lid types.LayerID, numATXs int, onAtx func(*types.ActivationTx) (*types.VerifiedActivationTx, error)) ([]*signing.EdSigner, []*types.ActivationTx) {
	tb.Helper()
	atxes := make([]*types.ActivationTx, 0, numATXs)
	signers := make([]*signing.EdSigner, 0, numATXs)
	for i := 0; i < numATXs; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(tb, err)
		signers = append(signers, signer)
		address := types.GenerateAddress(signer.PublicKey().Bytes())
		atx := types.NewActivationTx(
			types.NIPostChallenge{PublishEpoch: lid.GetEpoch()},
			address,
			nil,
			numUnit,
			nil,
			nil,
		)
		atx.SetEffectiveNumUnits(numUnit)
		atx.SetReceived(time.Now())
		require.NoError(tb, activation.SignAndFinalizeAtx(signer, atx))
		vAtx, err := onAtx(atx)
		require.NoError(tb, err)

		require.NoError(tb, atxs.Add(cdb, vAtx))
		atxes = append(atxes, atx)
	}
	return signers, atxes
}

func createProposals(
	t *testing.T,
	db sql.Executor,
	layerID types.LayerID,
	meshHash types.Hash32,
	signers []*signing.EdSigner,
	activeSet types.ATXIDList,
	txIDs []types.TransactionID,
) []*types.Proposal {
	t.Helper()
	numProposals := len(signers)
	// make sure proposals have overlapping transactions
	require.Zero(t, len(txIDs)%numProposals)
	pieSize := len(txIDs) / numProposals
	plist := make([]*types.Proposal, 0, numProposals)
	for i := 0; i < numProposals; i++ {
		from := i * pieSize
		to := from + (2 * pieSize) // every adjacent proposal will overlap pieSize of TXs
		if to > len(txIDs) {
			to = len(txIDs)
		}
		p := createProposal(t, db, activeSet, layerID, meshHash, activeSet[i], signers[i], txIDs[from:to], 1)
		plist = append(plist, p)
	}
	return plist
}

func createProposal(
	t *testing.T,
	db sql.Executor,
	activeSet types.ATXIDList,
	lid types.LayerID,
	meshHash types.Hash32,
	atxID types.ATXID,
	signer *signing.EdSigner,
	txIDs []types.TransactionID,
	numEligibility int,
) *types.Proposal {
	t.Helper()
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot: types.InnerBallot{
					Layer:     lid,
					AtxID:     atxID,
					EpochData: &types.EpochData{Beacon: types.RandomBeacon()},
				},
				EligibilityProofs: make([]types.VotingEligibility, numEligibility),
				ActiveSet:         activeSet,
			},
			TxIDs:    txIDs,
			MeshHash: meshHash,
		},
	}
	p.Ballot.Signature = signer.Sign(signing.BALLOT, p.Ballot.SignedBytes())
	p.Signature = signer.Sign(signing.BALLOT, p.SignedBytes())
	p.SmesherID = signer.NodeID()
	require.NoError(t, p.Initialize())
	require.NoError(t, ballots.Add(db, &p.Ballot))
	require.NoError(t, proposals.Add(db, p))
	return p
}

func checkRewards(t *testing.T, atxs []*types.ActivationTx, expWeightPer *big.Rat, rewards []types.AnyReward) {
	t.Helper()
	sort.Slice(atxs, func(i, j int) bool {
		return bytes.Compare(atxs[i].ID().Bytes(), atxs[j].ID().Bytes()) < 0
	})
	for i, r := range rewards {
		require.Equal(t, atxs[i].ID(), r.AtxID)
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

func genData(t *testing.T, cdb *datastore.CachedDB, lid types.LayerID, optimistic bool) hare.LayerOutput {
	numTXs := 1000
	numProposals := 10
	txIDs := createAndSaveTxs(t, numTXs, cdb)
	signers, atxes := createATXs(t, cdb, (lid.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	var meshHash types.Hash32
	if optimistic {
		meshHash = types.RandomHash()
	}
	require.NoError(t, layers.SetMeshHash(cdb, lid.Sub(1), meshHash))
	plist := createProposals(t, cdb, lid, meshHash, signers, activeSet, txIDs)
	return hare.LayerOutput{
		Ctx:       context.Background(),
		Layer:     lid,
		Proposals: types.ToProposalIDs(plist),
	}
}

func Test_SerialExecution(t *testing.T) {
	tg := createTestGenerator(t)
	tg.Start()
	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), gomock.Any()).AnyTimes()
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.cdb, layerID-1, types.EmptyBlockID))

	// hare output in the following order
	// layerID + 3
	// layerID + 1 (optimistic)
	// layerID + 2
	// layerID (optimistic)

	lid := layerID + 3
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any())
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), lid, gomock.Any())
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), lid, gomock.Any())
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lid, gomock.Any(), false)
	tg.mockPatrol.EXPECT().CompleteHare(lid)
	tg.hareCh <- genData(t, tg.cdb, lid, false)
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)

	// nothing happens
	tg.hareCh <- genData(t, tg.cdb, layerID+1, true)
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)

	lid = layerID + 2
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any())
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), lid, gomock.Any())
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), lid, gomock.Any())
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lid, gomock.Any(), false)
	tg.mockPatrol.EXPECT().CompleteHare(lid)
	tg.hareCh <- genData(t, tg.cdb, lid, false)
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)

	for _, lyr := range []types.LayerID{layerID, layerID + 1} {
		tg.mockExec.EXPECT().ExecuteOptimistic(gomock.Any(), lyr, uint64(baseTickHeight), gomock.Any(), gomock.Any()).
			Return(types.NewExistingBlock(types.RandomBlockID(), types.InnerBlock{LayerIndex: lyr}), nil)
		tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any())
		tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), lyr, gomock.Any())
		tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), lyr, gomock.Any())
		tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lyr, gomock.Any(), true)
		tg.mockPatrol.EXPECT().CompleteHare(lyr)
	}
	tg.hareCh <- genData(t, tg.cdb, layerID, true)
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_run(t *testing.T) {
	numTXs := 1000
	for _, tc := range []struct {
		desc       string
		gasLimit   uint64
		optimistic bool
		expNumTxs  int
	}{
		{
			desc:      "no consensus",
			gasLimit:  math.MaxUint64,
			expNumTxs: numTXs,
		},
		{
			desc:      "no consensus pruned",
			gasLimit:  defaultGas,
			expNumTxs: 1,
		},
		{
			desc:       "optimistic",
			gasLimit:   defaultGas,
			optimistic: true,
			expNumTxs:  numTXs,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			tg := createTestGenerator(t)
			tg.cfg.BlockGasLimit = tc.gasLimit
			layerID := types.GetEffectiveGenesis().Add(100)
			require.NoError(t, layers.SetApplied(tg.cdb, layerID-1, types.EmptyBlockID))
			var meshHash types.Hash32
			if tc.optimistic {
				meshHash = types.RandomHash()
			}
			require.NoError(t, layers.SetMeshHash(tg.cdb, layerID.Sub(1), meshHash))
			// create multiple proposals with overlapping TXs
			numProposals := 10
			txIDs := createAndSaveTxs(t, numTXs, tg.cdb)
			signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
			activeSet := types.ToATXIDs(atxes)
			plist := createProposals(t, tg.cdb, layerID, meshHash, signers, activeSet, txIDs)
			pids := types.ToProposalIDs(plist)
			tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids)

			var block *types.Block
			if tc.optimistic {
				tg.mockExec.EXPECT().ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, lid types.LayerID, tickHeight uint64, rewards []types.AnyReward, tids []types.TransactionID) (*types.Block, error) {
						require.Len(t, tids, len(txIDs))
						block = &types.Block{
							InnerBlock: types.InnerBlock{
								LayerIndex: lid,
								TickHeight: tickHeight,
								Rewards:    rewards,
								TxIDs:      tids,
							},
						}
						block.Initialize()
						return block, nil
					})
			}
			tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, got *types.Block) error {
					if tc.optimistic {
						require.Equal(t, block, got)
					} else {
						block = got
					}
					require.NotEqual(t, types.EmptyBlockID, block.ID())
					require.Equal(t, layerID, block.LayerIndex)
					require.Len(t, block.TxIDs, tc.expNumTxs)
					require.Len(t, block.Rewards, numProposals)
					// numUnit is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1 eligibility
					// the expected weight for each eligibility is `numUnit` * 1/3
					expWeight := new(big.Rat).SetInt64(numUnit * 1 / 3)
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
			tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, gomock.Any(), tc.optimistic).DoAndReturn(
				func(_ context.Context, _ types.LayerID, got types.BlockID, _ bool) error {
					require.Equal(t, block.ID(), got)
					return nil
				})
			tg.mockPatrol.EXPECT().CompleteHare(layerID)
			tg.Start()
			tg.hareCh <- hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}
			require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
			tg.Stop()
		})
	}
}

func Test_processHareOutput_EmptyOutput(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.cdb, layerID-1, types.EmptyBlockID))
	tg.Start()
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, types.EmptyBlockID)
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), layerID, types.EmptyBlockID)
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, types.EmptyBlockID, false)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	tg.hareCh <- hare.LayerOutput{Ctx: context.Background(), Layer: layerID}
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_run_FetchFailed(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.cdb, layerID-1, types.EmptyBlockID))
	tg.Start()
	pids := []types.ProposalID{{1}, {2}, {3}}
	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids).DoAndReturn(
		func(_ context.Context, _ []types.ProposalID) error {
			return errors.New("unknown")
		})
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	tg.hareCh <- hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_run_DiffHasFromConsensus(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.cdb, layerID-1, types.EmptyBlockID))
	tg.Start()

	// create multiple proposals with overlapping TXs
	txIDs := createAndSaveTxs(t, 100, tg.cdb)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	meshHash := types.RandomHash()
	plist := createProposals(t, tg.cdb, layerID, meshHash, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	require.NoError(t, layers.SetMeshHash(tg.cdb, layerID.Sub(1), types.RandomHash()))

	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	tg.hareCh <- hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_run_ExecuteFailed(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.cdb, layerID-1, types.EmptyBlockID))
	tg.Start()
	txIDs := createAndSaveTxs(t, 100, tg.cdb)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	meshHash := types.RandomHash()
	plist := createProposals(t, tg.cdb, layerID, meshHash, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	require.NoError(t, layers.SetMeshHash(tg.cdb, layerID.Sub(1), meshHash))

	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids)
	tg.mockExec.EXPECT().ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, lid types.LayerID, tickHeight uint64, rewards []types.AnyReward, tids []types.TransactionID) (*types.Block, error) {
			require.Len(t, tids, len(txIDs))
			return nil, errors.New("unknown")
		})
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	tg.hareCh <- hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_run_AddBlockFailed(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.cdb, layerID-1, types.EmptyBlockID))
	tg.Start()
	txIDs := createAndSaveTxs(t, 100, tg.cdb)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	meshHash := types.RandomHash()
	plist := createProposals(t, tg.cdb, layerID, meshHash, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	require.NoError(t, layers.SetMeshHash(tg.cdb, layerID.Sub(1), meshHash))

	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids)
	block := types.NewExistingBlock(types.BlockID{1, 2, 3}, types.InnerBlock{LayerIndex: layerID})
	tg.mockExec.EXPECT().ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).Return(block, nil)
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any()).Return(errors.New("unknown"))
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	tg.hareCh <- hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_run_RegisterCertFailureIgnored(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.cdb, layerID-1, types.EmptyBlockID))
	tg.Start()
	txIDs := createAndSaveTxs(t, 100, tg.cdb)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	meshHash := types.RandomHash()
	plist := createProposals(t, tg.cdb, layerID, meshHash, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	require.NoError(t, layers.SetMeshHash(tg.cdb, layerID.Sub(1), meshHash))

	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids)
	block := types.NewExistingBlock(types.BlockID{1, 2, 3}, types.InnerBlock{LayerIndex: layerID})
	tg.mockExec.EXPECT().ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).Return(block, nil)
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any())
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, gomock.Any()).Return(errors.New("unknown"))
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), layerID, gomock.Any())
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, block.ID(), true)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	tg.hareCh <- hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_run_CertifyFailureIgnored(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.cdb, layerID-1, types.EmptyBlockID))
	tg.Start()
	txIDs := createAndSaveTxs(t, 100, tg.cdb)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	meshHash := types.RandomHash()
	plist := createProposals(t, tg.cdb, layerID, meshHash, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	require.NoError(t, layers.SetMeshHash(tg.cdb, layerID.Sub(1), meshHash))

	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids)
	block := types.NewExistingBlock(types.BlockID{1, 2, 3}, types.InnerBlock{LayerIndex: layerID})
	tg.mockExec.EXPECT().ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).Return(block, nil)
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any())
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, gomock.Any())
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), layerID, gomock.Any()).Return(errors.New("unknown"))
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, block.ID(), true)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	tg.hareCh <- hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_run_ProcessLayerFailed(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.cdb, layerID-1, types.EmptyBlockID))
	tg.Start()
	txIDs := createAndSaveTxs(t, 100, tg.cdb)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	meshHash := types.RandomHash()
	plist := createProposals(t, tg.cdb, layerID, meshHash, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	require.NoError(t, layers.SetMeshHash(tg.cdb, layerID.Sub(1), meshHash))

	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids)
	block := types.NewExistingBlock(types.BlockID{1, 2, 3}, types.InnerBlock{LayerIndex: layerID})
	tg.mockExec.EXPECT().ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).Return(block, nil)
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any())
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, gomock.Any())
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), layerID, gomock.Any())
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, block.ID(), true).Return(errors.New("unknown"))
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	tg.hareCh <- hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_processHareOutput_UnequalHeight(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	numProposals := 10
	rng := rand.New(rand.NewSource(10101))
	maxHeight := uint64(0)
	signers, atxes := createModifiedATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals, func(atx *types.ActivationTx) (*types.VerifiedActivationTx, error) {
		n := rng.Uint64()
		if n > maxHeight {
			maxHeight = n
		}
		return atx.Verify(n, 1)
	})
	activeSet := types.ToATXIDs(atxes)
	pList := createProposals(t, tg.cdb, layerID, types.Hash32{}, signers, activeSet, nil)
	ho := hare.LayerOutput{
		Ctx:       context.Background(),
		Layer:     layerID,
		Proposals: types.ToProposalIDs(pList),
	}
	tg.mockFetch.EXPECT().GetProposals(ho.Ctx, ho.Proposals)
	var block *types.Block
	tg.mockMesh.EXPECT().AddBlockWithTXs(ho.Ctx, gomock.Any()).DoAndReturn(
		func(_ context.Context, b *types.Block) error {
			block = b
			return nil
		})
	tg.mockCert.EXPECT().RegisterForCert(ho.Ctx, layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, bid types.BlockID) error {
			require.Equal(t, block.ID(), bid)
			return nil
		})
	tg.mockCert.EXPECT().CertifyIfEligible(ho.Ctx, gomock.Any(), layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ log.Log, _ types.LayerID, bid types.BlockID) error {
			require.Equal(t, block.ID(), bid)
			return eligibility.ErrNotActive
		})
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, gomock.Any(), false).DoAndReturn(
		func(_ context.Context, _ types.LayerID, bid types.BlockID, _ bool) error {
			require.Equal(t, block.ID(), bid)
			return nil
		})
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	got, err := tg.processHareOutput(ho)
	require.NoError(t, err)
	require.Equal(t, block, got)
	require.Equal(t, maxHeight, got.TickHeight)
	require.Len(t, got.Rewards, numProposals)
	// numUnit is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1 eligibility
	// the expected weight for each eligibility is `numUnit` * 1/3
	expWeight := new(big.Rat).SetInt64(numUnit * 1 / 3)
	checkRewards(t, atxes, expWeight, got.Rewards)
}

func Test_processHareOutput_bad_state(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), 1)
	activeSet := types.ToATXIDs(atxes)

	t.Run("tx missing", func(t *testing.T) {
		p := createProposal(t, tg.cdb, activeSet, layerID, types.Hash32{}, activeSet[0], signers[0], []types.TransactionID{types.RandomTransactionID()}, 1)
		ho := hare.LayerOutput{
			Ctx:       context.Background(),
			Layer:     layerID,
			Proposals: types.ToProposalIDs([]*types.Proposal{p}),
		}
		tg.mockFetch.EXPECT().GetProposals(ho.Ctx, ho.Proposals)
		tg.mockPatrol.EXPECT().CompleteHare(layerID)
		got, err := tg.processHareOutput(ho)
		require.ErrorIs(t, err, errProposalTxMissing)
		require.Nil(t, got)
	})

	t.Run("tx header missing", func(t *testing.T) {
		tx := types.Transaction{
			RawTx: types.NewRawTx([]byte{1, 1, 1}),
		}
		require.NoError(t, transactions.Add(tg.cdb, &tx, time.Now()))
		p := createProposal(t, tg.cdb, activeSet, layerID, types.Hash32{}, activeSet[0], signers[0], []types.TransactionID{tx.ID}, 1)
		ho := hare.LayerOutput{
			Ctx:       context.Background(),
			Layer:     layerID,
			Proposals: types.ToProposalIDs([]*types.Proposal{p}),
		}
		tg.mockFetch.EXPECT().GetProposals(ho.Ctx, ho.Proposals)
		tg.mockPatrol.EXPECT().CompleteHare(layerID)
		got, err := tg.processHareOutput(ho)
		require.ErrorIs(t, err, errProposalTxHdrMissing)
		require.Nil(t, got)
	})
}

func Test_processHareOutput_EmptyProposals(t *testing.T) {
	tg := createTestGenerator(t)
	numProposals := 10
	lid := types.GetEffectiveGenesis().Add(20)
	signers, atxes := createATXs(t, tg.cdb, (lid.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	plist := make([]*types.Proposal, 0, numProposals)
	for i := 0; i < numProposals; i++ {
		p := createProposal(t, tg.cdb, activeSet, lid, types.Hash32{}, activeSet[i], signers[i], nil, 1)
		plist = append(plist, p)
	}
	ho := hare.LayerOutput{
		Ctx:       context.Background(),
		Layer:     lid,
		Proposals: types.ToProposalIDs(plist),
	}
	tg.mockFetch.EXPECT().GetProposals(ho.Ctx, ho.Proposals)
	var block *types.Block
	tg.mockMesh.EXPECT().AddBlockWithTXs(ho.Ctx, gomock.Any()).DoAndReturn(
		func(_ context.Context, b *types.Block) error {
			block = b
			return nil
		})
	tg.mockCert.EXPECT().RegisterForCert(ho.Ctx, lid, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, bid types.BlockID) error {
			require.Equal(t, block.ID(), bid)
			return nil
		})
	tg.mockCert.EXPECT().CertifyIfEligible(ho.Ctx, gomock.Any(), lid, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ log.Log, _ types.LayerID, bid types.BlockID) error {
			require.Equal(t, block.ID(), bid)
			return eligibility.ErrNotActive
		})
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lid, gomock.Any(), false).DoAndReturn(
		func(_ context.Context, _ types.LayerID, bid types.BlockID, _ bool) error {
			require.Equal(t, block.ID(), bid)
			return nil
		})
	tg.mockPatrol.EXPECT().CompleteHare(lid)
	got, err := tg.processHareOutput(ho)
	require.NoError(t, err)
	require.Equal(t, block, got)
	require.Equal(t, lid, got.LayerIndex)
	require.Empty(t, got.TxIDs)
	require.Len(t, got.Rewards, numProposals)
	// numUint is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1 eligibility
	// the expected weight for each eligibility is `numUnit` * 1/3
	expWeight := new(big.Rat).SetInt64(numUnit * 1 / 3)
	checkRewards(t, atxes, expWeight, got.Rewards)
}

func Test_processHareOutput_StableBlockID(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	numTXs := 1000
	numProposals := 10
	txIDs := createAndSaveTxs(t, numTXs, tg.cdb)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	plist := createProposals(t, tg.cdb, layerID, types.Hash32{}, signers, activeSet, txIDs)
	ho1 := hare.LayerOutput{
		Ctx:       context.Background(),
		Layer:     layerID,
		Proposals: types.ToProposalIDs(plist),
	}
	tg.mockFetch.EXPECT().GetProposals(ho1.Ctx, ho1.Proposals)
	tg.mockMesh.EXPECT().AddBlockWithTXs(ho1.Ctx, gomock.Any())
	tg.mockCert.EXPECT().RegisterForCert(ho1.Ctx, layerID, gomock.Any())
	tg.mockCert.EXPECT().CertifyIfEligible(ho1.Ctx, gomock.Any(), layerID, gomock.Any()).Return(eligibility.ErrNotActive)
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, gomock.Any(), false)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	got1, err := tg.processHareOutput(ho1)
	require.NoError(t, err)
	require.NotNil(t, got1)
	require.Equal(t, layerID, got1.LayerIndex)
	require.Len(t, got1.TxIDs, numTXs)

	// reorder the proposals
	ordered := plist[numProposals/2 : numProposals]
	ordered = append(ordered, plist[0:numProposals/2]...)
	require.NotEqual(t, plist, ordered)
	ho2 := hare.LayerOutput{
		Ctx:       context.Background(),
		Layer:     layerID,
		Proposals: types.ToProposalIDs(ordered),
	}
	tg.mockFetch.EXPECT().GetProposals(ho2.Ctx, ho2.Proposals)
	tg.mockMesh.EXPECT().AddBlockWithTXs(ho2.Ctx, gomock.Any())
	tg.mockCert.EXPECT().RegisterForCert(ho2.Ctx, layerID, gomock.Any())
	tg.mockCert.EXPECT().CertifyIfEligible(ho2.Ctx, gomock.Any(), layerID, gomock.Any()).Return(eligibility.ErrNotActive)
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, gomock.Any(), false)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	got2, err := tg.processHareOutput(ho2)
	require.NoError(t, err)
	require.NotNil(t, got2)
	require.Equal(t, got1, got2)
}

func Test_processHareOutput_SameATX(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	numTXs := 1000
	numProposals := 10
	txIDs := createAndSaveTxs(t, numTXs, tg.cdb)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	atxID := activeSet[0]
	proposal1 := createProposal(t, tg.cdb, activeSet, layerID, types.Hash32{}, atxID, signers[0], txIDs[0:500], 1)
	proposal2 := createProposal(t, tg.cdb, activeSet, layerID, types.Hash32{}, atxID, signers[0], txIDs[400:], 1)
	plist := []*types.Proposal{proposal1, proposal2}
	ho := hare.LayerOutput{
		Ctx:       context.Background(),
		Layer:     layerID,
		Proposals: types.ToProposalIDs(plist),
	}
	tg.mockFetch.EXPECT().GetProposals(ho.Ctx, ho.Proposals)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	got, err := tg.processHareOutput(ho)
	require.ErrorIs(t, err, errDuplicateATX)
	require.Nil(t, got)
}

func Test_processHareOutput_EmptyATXID(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	numTXs := 1000
	numProposals := 10
	txIDs := createAndSaveTxs(t, numTXs, tg.cdb)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	plist := createProposals(t, tg.cdb, layerID, types.Hash32{}, signers[1:], activeSet[1:], txIDs[1:])
	p := createProposal(t, tg.cdb, activeSet, layerID, types.Hash32{}, types.EmptyATXID, signers[0], txIDs, 1)
	plist = append(plist, p)
	ho := hare.LayerOutput{
		Ctx:       context.Background(),
		Layer:     layerID,
		Proposals: types.ToProposalIDs(plist),
	}
	tg.mockFetch.EXPECT().GetProposals(ho.Ctx, ho.Proposals)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	got, err := tg.processHareOutput(ho)
	require.ErrorIs(t, err, errInvalidATXID)
	require.Nil(t, got)
}

func Test_processHareOutput_MultipleEligibilities(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	ids := createAndSaveTxs(t, 1000, tg.cdb)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	plist := []*types.Proposal{
		createProposal(t, tg.cdb, activeSet, layerID, types.Hash32{}, atxes[0].ID(), signers[0], ids, 2),
		createProposal(t, tg.cdb, activeSet, layerID, types.Hash32{}, atxes[1].ID(), signers[1], ids, 1),
		createProposal(t, tg.cdb, activeSet, layerID, types.Hash32{}, atxes[2].ID(), signers[2], ids, 5),
	}
	ho := hare.LayerOutput{
		Ctx:       context.Background(),
		Layer:     layerID,
		Proposals: types.ToProposalIDs(plist),
	}
	tg.mockFetch.EXPECT().GetProposals(ho.Ctx, ho.Proposals)
	tg.mockMesh.EXPECT().AddBlockWithTXs(ho.Ctx, gomock.Any())
	tg.mockCert.EXPECT().RegisterForCert(ho.Ctx, layerID, gomock.Any())
	tg.mockCert.EXPECT().CertifyIfEligible(ho.Ctx, gomock.Any(), layerID, gomock.Any()).Return(eligibility.ErrNotActive)
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, gomock.Any(), false)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	got, err := tg.processHareOutput(ho)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.Rewards, len(plist))
	sort.Slice(plist, func(i, j int) bool {
		return bytes.Compare(plist[i].AtxID.Bytes(), plist[j].AtxID.Bytes()) < 0
	})
	totalWeight := new(big.Rat)
	for i, r := range got.Rewards {
		require.Equal(t, plist[i].AtxID, r.AtxID)
		got := r.Weight.ToBigRat()
		// numUint is the ATX weight. eligible slots per epoch is 3 for each atx
		// the expected weight for each eligibility is `numUnit` * 1/3
		require.Equal(t, new(big.Rat).SetInt64(numUnit*1/3*int64(len(plist[i].EligibilityProofs))), got)
		totalWeight.Add(totalWeight, got)
	}
	require.Equal(t, new(big.Rat).SetInt64(numUnit*8*1/3), totalWeight)
}
