package blocks

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/hare3/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare4"
	"github.com/spacemeshos/go-spacemesh/proposals/store"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	numUnit        = 12
	defaultGas     = 100
	baseTickHeight = 3

	layerSize = 10
	epochSize = 3
)

func testConfig() Config {
	return Config{
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
	hareCh     chan hare4.ConsensusOutput
}

func createTestGenerator(t *testing.T) *testGenerator {
	types.SetLayersPerEpoch(3)
	ctrl := gomock.NewController(t)
	ch := make(chan hare4.ConsensusOutput, 100)
	tg := &testGenerator{
		mockMesh:   mocks.NewMockmeshProvider(ctrl),
		mockExec:   mocks.NewMockexecutor(ctrl),
		mockFetch:  smocks.NewMockProposalFetcher(ctrl),
		mockCert:   mocks.NewMockcertifier(ctrl),
		mockPatrol: mocks.NewMocklayerPatrol(ctrl),
		hareCh:     ch,
	}
	tg.mockMesh.EXPECT().ProcessedLayer().Return(types.LayerID(1)).AnyTimes()
	lg := zaptest.NewLogger(t)
	db := sql.InMemory()
	data := atxsdata.New()
	proposals := store.New()
	tg.Generator = NewGenerator(
		db,
		data,
		proposals,
		tg.mockExec,
		tg.mockMesh,
		tg.mockFetch,
		tg.mockCert,
		tg.mockPatrol,
		WithGeneratorLogger(lg),
		WithHareOutputChan(ch),
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

func createATXs(
	t *testing.T,
	data *atxsdata.Data,
	lid types.LayerID,
	numATXs int,
) ([]*signing.EdSigner, []*types.ActivationTx) {
	return createModifiedATXs(
		t,
		data,
		lid,
		numATXs,
		func(atx *types.ActivationTx) {
			atx.BaseTickHeight = baseTickHeight
			atx.TickCount = 1
		},
	)
}

func createModifiedATXs(
	tb testing.TB,
	data *atxsdata.Data,
	lid types.LayerID,
	numATXs int,
	onAtx func(*types.ActivationTx),
) ([]*signing.EdSigner, []*types.ActivationTx) {
	tb.Helper()
	atxes := make([]*types.ActivationTx, 0, numATXs)
	signers := make([]*signing.EdSigner, 0, numATXs)
	for i := 0; i < numATXs; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(tb, err)
		signers = append(signers, signer)
		atx := &types.ActivationTx{
			PublishEpoch: lid.GetEpoch(),
			Coinbase:     types.GenerateAddress(signer.PublicKey().Bytes()),
			NumUnits:     numUnit,
			SmesherID:    signer.NodeID(),
			TickCount:    1,
			Weight:       uint64(numUnit),
		}
		atx.SetReceived(time.Now())
		atx.SetID(types.RandomATXID())
		onAtx(atx)
		data.AddFromAtx(atx, false)
		atxes = append(atxes, atx)
	}
	return signers, atxes
}

func createProposals(
	t *testing.T,
	db sql.Executor,
	store *store.Store,
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
		p := createProposal(t, db, store, activeSet, layerID, meshHash, activeSet[i], signers[i], txIDs[from:to], 1)
		plist = append(plist, p)
	}
	return plist
}

func createProposal(
	t *testing.T,
	db sql.Executor,
	store *store.Store,
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
					Layer: lid,
					AtxID: atxID,
					EpochData: &types.EpochData{
						Beacon:           types.RandomBeacon(),
						EligibilityCount: uint32(layerSize * epochSize / len(activeSet)),
						ActiveSetHash:    activeSet.Hash(),
					},
				},
				EligibilityProofs: make([]types.VotingEligibility, numEligibility),
			},
			TxIDs:    txIDs,
			MeshHash: meshHash,
		},
	}
	p.Ballot.Signature = signer.Sign(signing.BALLOT, p.Ballot.SignedBytes())
	p.Signature = signer.Sign(signing.PROPOSAL, p.SignedBytes())
	p.SmesherID = signer.NodeID()
	require.NoError(t, p.Initialize())
	require.NoError(t, ballots.Add(db, &p.Ballot))
	store.Add(p)
	activesets.Add(db, activeSet.Hash(), &types.EpochActiveSet{Set: activeSet})
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
	tg.Start(context.Background())
	tg.Start(context.Background()) // start for the second time is ok.
	tg.Stop()
}

func Test_StopBeforeStart(t *testing.T) {
	tg := createTestGenerator(t)
	tg.Stop()
}

func genData(
	t *testing.T,
	db *sql.Database,
	data *atxsdata.Data,
	store *store.Store,
	lid types.LayerID,
	optimistic bool,
) hare4.ConsensusOutput {
	numTXs := 1000
	numProposals := 10
	txIDs := createAndSaveTxs(t, numTXs, db)
	signers, atxes := createATXs(t, data, (lid.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	var meshHash types.Hash32
	if optimistic {
		meshHash = types.RandomHash()
	}
	require.NoError(t, layers.SetMeshHash(db, lid.Sub(1), meshHash))
	plist := createProposals(t, db, store, lid, meshHash, signers, activeSet, txIDs)
	return hare4.ConsensusOutput{
		Layer:     lid,
		Proposals: types.ToProposalIDs(plist),
	}
}

func Test_SerialExecution(t *testing.T) {
	tg := createTestGenerator(t)
	tg.Start(context.Background())
	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), gomock.Any()).AnyTimes()
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.db, layerID-1, types.EmptyBlockID))

	// hare output in the following order
	// layerID + 3
	// layerID + 1 (optimistic)
	// layerID + 2
	// layerID (optimistic)

	lid := layerID + 3
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any())
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), lid, gomock.Any())
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), lid, gomock.Any())
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lid, gomock.Any(), false)
	tg.mockPatrol.EXPECT().CompleteHare(lid)
	tg.hareCh <- genData(t, tg.db, tg.atxs, tg.proposals, lid, false)
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)

	// nothing happens
	tg.hareCh <- genData(t, tg.db, tg.atxs, tg.proposals, layerID+1, true)
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)

	lid = layerID + 2
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any())
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), lid, gomock.Any())
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), lid, gomock.Any())
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lid, gomock.Any(), false)
	tg.mockPatrol.EXPECT().CompleteHare(lid)
	tg.hareCh <- genData(t, tg.db, tg.atxs, tg.proposals, lid, false)
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)

	for _, lyr := range []types.LayerID{layerID, layerID + 1} {
		tg.mockExec.EXPECT().ExecuteOptimistic(gomock.Any(), lyr, uint64(baseTickHeight), gomock.Any(), gomock.Any()).
			Return(types.NewExistingBlock(types.RandomBlockID(), types.InnerBlock{LayerIndex: lyr}), nil)
		tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any())
		tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), lyr, gomock.Any())
		tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), lyr, gomock.Any())
		tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lyr, gomock.Any(), true)
		tg.mockPatrol.EXPECT().CompleteHare(lyr)
	}
	tg.hareCh <- genData(t, tg.db, tg.atxs, tg.proposals, layerID, true)
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
		t.Run(tc.desc, func(t *testing.T) {
			tg := createTestGenerator(t)
			tg.cfg.BlockGasLimit = tc.gasLimit
			tg.mockMesh = mocks.NewMockmeshProvider(gomock.NewController(t))
			tg.msh = tg.mockMesh
			layerID := types.GetEffectiveGenesis().Add(100)
			processed := layerID - 1
			tg.mockMesh.EXPECT().ProcessedLayer().DoAndReturn(func() types.LayerID { return processed }).AnyTimes()
			require.NoError(t, layers.SetApplied(tg.db, layerID-1, types.EmptyBlockID))
			var meshHash types.Hash32
			if tc.optimistic {
				meshHash = types.RandomHash()
			}
			require.NoError(t, layers.SetMeshHash(tg.db, layerID.Sub(1), meshHash))
			// create multiple proposals with overlapping TXs
			numProposals := 10
			txIDs := createAndSaveTxs(t, numTXs, tg.db)
			signers, atxes := createATXs(t, tg.atxs, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
			activeSet := types.ToATXIDs(atxes)
			plist := createProposals(t, tg.db, tg.proposals, layerID, meshHash, signers, activeSet, txIDs)
			pids := types.ToProposalIDs(plist)
			tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids)

			var block *types.Block
			if tc.optimistic {
				tg.mockExec.EXPECT().
					ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).
					DoAndReturn(
						func(
							_ context.Context,
							lid types.LayerID,
							tickHeight uint64,
							rewards []types.AnyReward,
							tids []types.TransactionID,
						) (*types.Block, error) {
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
					// numUnit is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1
					// eligibility; the expected weight for each eligibility is `numUnit` * 1/3
					expWeight := new(big.Rat).SetInt64(numUnit * 1 / 3)
					checkRewards(t, atxes, expWeight, block.Rewards)
					return nil
				})
			tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ types.LayerID, got types.BlockID) error {
					require.Equal(t, block.ID(), got)
					return nil
				})
			tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), layerID, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ types.LayerID, got types.BlockID) error {
					require.Equal(t, block.ID(), got)
					return nil
				})
			tg.mockMesh.EXPECT().
				ProcessLayerPerHareOutput(gomock.Any(), layerID, gomock.Any(), tc.optimistic).
				DoAndReturn(
					func(_ context.Context, _ types.LayerID, got types.BlockID, _ bool) error {
						require.Equal(t, block.ID(), got)
						processed = layerID
						return nil
					})
			tg.mockPatrol.EXPECT().CompleteHare(layerID)
			tg.Start(context.Background())
			tg.hareCh <- hare4.ConsensusOutput{Layer: layerID, Proposals: pids}
			require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
			tg.Stop()
		})
	}
}

func Test_processHareOutput_EmptyOutput(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.db, layerID-1, types.EmptyBlockID))
	tg.Start(context.Background())
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, types.EmptyBlockID)
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), layerID, types.EmptyBlockID)
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, types.EmptyBlockID, false)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	tg.hareCh <- hare4.ConsensusOutput{Layer: layerID}
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_run_FetchFailed(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.db, layerID-1, types.EmptyBlockID))
	tg.Start(context.Background())
	pids := []types.ProposalID{{1}, {2}, {3}}
	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids).DoAndReturn(
		func(_ context.Context, _ []types.ProposalID) error {
			return errors.New("unknown")
		})
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	tg.hareCh <- hare4.ConsensusOutput{Layer: layerID, Proposals: pids}
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_run_DiffHasFromConsensus(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.db, layerID-1, types.EmptyBlockID))
	tg.Start(context.Background())

	// create multiple proposals with overlapping TXs
	txIDs := createAndSaveTxs(t, 100, tg.db)
	signers, atxes := createATXs(t, tg.atxs, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	meshHash := types.RandomHash()
	plist := createProposals(t, tg.db, tg.proposals, layerID, meshHash, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	require.NoError(t, layers.SetMeshHash(tg.db, layerID.Sub(1), types.RandomHash()))

	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	tg.hareCh <- hare4.ConsensusOutput{Layer: layerID, Proposals: pids}
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_run_ExecuteFailed(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.db, layerID-1, types.EmptyBlockID))
	tg.Start(context.Background())
	txIDs := createAndSaveTxs(t, 100, tg.db)
	signers, atxes := createATXs(t, tg.atxs, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	meshHash := types.RandomHash()
	plist := createProposals(t, tg.db, tg.proposals, layerID, meshHash, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	require.NoError(t, layers.SetMeshHash(tg.db, layerID.Sub(1), meshHash))

	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids)
	tg.mockExec.EXPECT().
		ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).
		DoAndReturn(
			func(
				_ context.Context,
				lid types.LayerID,
				tickHeight uint64,
				rewards []types.AnyReward,
				tids []types.TransactionID,
			) (*types.Block, error) {
				require.Len(t, tids, len(txIDs))
				return nil, errors.New("unknown")
			})
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	tg.hareCh <- hare4.ConsensusOutput{Layer: layerID, Proposals: pids}
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_run_AddBlockFailed(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.db, layerID-1, types.EmptyBlockID))
	tg.Start(context.Background())
	txIDs := createAndSaveTxs(t, 100, tg.db)
	signers, atxes := createATXs(t, tg.atxs, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	meshHash := types.RandomHash()
	plist := createProposals(t, tg.db, tg.proposals, layerID, meshHash, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	require.NoError(t, layers.SetMeshHash(tg.db, layerID.Sub(1), meshHash))

	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids)
	block := types.NewExistingBlock(types.BlockID{1, 2, 3}, types.InnerBlock{LayerIndex: layerID})
	tg.mockExec.EXPECT().
		ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).
		Return(block, nil)
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any()).Return(errors.New("unknown"))
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	tg.hareCh <- hare4.ConsensusOutput{Layer: layerID, Proposals: pids}
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_run_RegisterCertFailureIgnored(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.db, layerID-1, types.EmptyBlockID))
	tg.Start(context.Background())
	txIDs := createAndSaveTxs(t, 100, tg.db)
	signers, atxes := createATXs(t, tg.atxs, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	meshHash := types.RandomHash()
	plist := createProposals(t, tg.db, tg.proposals, layerID, meshHash, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	require.NoError(t, layers.SetMeshHash(tg.db, layerID.Sub(1), meshHash))

	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids)
	block := types.NewExistingBlock(types.BlockID{1, 2, 3}, types.InnerBlock{LayerIndex: layerID})
	tg.mockExec.EXPECT().
		ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).
		Return(block, nil)
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any())
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, gomock.Any()).Return(errors.New("unknown"))
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), layerID, gomock.Any())
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, block.ID(), true)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	tg.hareCh <- hare4.ConsensusOutput{Layer: layerID, Proposals: pids}
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_run_CertifyFailureIgnored(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.db, layerID-1, types.EmptyBlockID))
	tg.Start(context.Background())
	txIDs := createAndSaveTxs(t, 100, tg.db)
	signers, atxes := createATXs(t, tg.atxs, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	meshHash := types.RandomHash()
	plist := createProposals(t, tg.db, tg.proposals, layerID, meshHash, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	require.NoError(t, layers.SetMeshHash(tg.db, layerID.Sub(1), meshHash))

	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids)
	block := types.NewExistingBlock(types.BlockID{1, 2, 3}, types.InnerBlock{LayerIndex: layerID})
	tg.mockExec.EXPECT().
		ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).
		Return(block, nil)
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any())
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, gomock.Any())
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), layerID, gomock.Any()).Return(errors.New("unknown"))
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, block.ID(), true)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	tg.hareCh <- hare4.ConsensusOutput{Layer: layerID, Proposals: pids}
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_run_ProcessLayerFailed(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	require.NoError(t, layers.SetApplied(tg.db, layerID-1, types.EmptyBlockID))
	tg.Start(context.Background())
	txIDs := createAndSaveTxs(t, 100, tg.db)
	signers, atxes := createATXs(t, tg.atxs, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	meshHash := types.RandomHash()
	plist := createProposals(t, tg.db, tg.proposals, layerID, meshHash, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)
	require.NoError(t, layers.SetMeshHash(tg.db, layerID.Sub(1), meshHash))

	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids)
	block := types.NewExistingBlock(types.BlockID{1, 2, 3}, types.InnerBlock{LayerIndex: layerID})
	tg.mockExec.EXPECT().
		ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).
		Return(block, nil)
	tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any())
	tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, gomock.Any())
	tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), layerID, gomock.Any())
	tg.mockMesh.EXPECT().
		ProcessLayerPerHareOutput(gomock.Any(), layerID, block.ID(), true).
		Return(errors.New("unknown"))
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	tg.hareCh <- hare4.ConsensusOutput{Layer: layerID, Proposals: pids}
	require.Eventually(t, func() bool { return len(tg.hareCh) == 0 }, time.Second, 100*time.Millisecond)
	tg.Stop()
}

func Test_processHareOutput_UnequalHeight(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	numProposals := 10
	var seed [32]byte
	binary.LittleEndian.PutUint64(seed[:], 10101)
	rng := rand.New(rand.NewChaCha8(seed))
	maxHeight := uint64(0)
	signers, atxes := createModifiedATXs(
		t,
		tg.atxs,
		(layerID.GetEpoch() - 1).FirstLayer(),
		numProposals,
		func(atx *types.ActivationTx) {
			atx.TickCount = 1
			atx.BaseTickHeight = rng.Uint64()
			maxHeight = max(maxHeight, atx.BaseTickHeight)
		},
	)
	activeSet := types.ToATXIDs(atxes)
	pList := createProposals(t, tg.db, tg.proposals, layerID, types.Hash32{}, signers, activeSet, nil)
	ctx := context.Background()
	ho := hare4.ConsensusOutput{
		Layer:     layerID,
		Proposals: types.ToProposalIDs(pList),
	}
	tg.mockFetch.EXPECT().GetProposals(ctx, ho.Proposals)
	var block *types.Block
	tg.mockMesh.EXPECT().AddBlockWithTXs(ctx, gomock.Any()).DoAndReturn(
		func(_ context.Context, b *types.Block) error {
			block = b
			return nil
		})
	tg.mockCert.EXPECT().RegisterForCert(ctx, layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, bid types.BlockID) error {
			require.Equal(t, block.ID(), bid)
			return nil
		})
	tg.mockCert.EXPECT().CertifyIfEligible(ctx, layerID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, bid types.BlockID) error {
			require.Equal(t, block.ID(), bid)
			return eligibility.ErrNotActive
		})
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, gomock.Any(), false).DoAndReturn(
		func(_ context.Context, _ types.LayerID, bid types.BlockID, _ bool) error {
			require.Equal(t, block.ID(), bid)
			return nil
		})
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	got, err := tg.processHareOutput(ctx, ho)
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
	signers, atxes := createATXs(t, tg.atxs, (layerID.GetEpoch() - 1).FirstLayer(), 1)
	activeSet := types.ToATXIDs(atxes)

	t.Run("tx missing", func(t *testing.T) {
		p := createProposal(
			t,
			tg.db,
			tg.proposals,
			activeSet,
			layerID,
			types.Hash32{},
			activeSet[0],
			signers[0],
			[]types.TransactionID{types.RandomTransactionID()},
			1,
		)
		ho := hare4.ConsensusOutput{
			Layer:     layerID,
			Proposals: types.ToProposalIDs([]*types.Proposal{p}),
		}
		tg.mockFetch.EXPECT().GetProposals(gomock.Any(), ho.Proposals)
		tg.mockPatrol.EXPECT().CompleteHare(layerID)
		got, err := tg.processHareOutput(context.Background(), ho)
		require.ErrorIs(t, err, errProposalTxMissing)
		require.Nil(t, got)
	})

	t.Run("tx header missing", func(t *testing.T) {
		tx := types.Transaction{
			RawTx: types.NewRawTx([]byte{1, 1, 1}),
		}
		require.NoError(t, transactions.Add(tg.db, &tx, time.Now()))
		p := createProposal(
			t,
			tg.db,
			tg.proposals,
			activeSet,
			layerID,
			types.Hash32{},
			activeSet[0],
			signers[0],
			[]types.TransactionID{tx.ID},
			1,
		)
		ctx := context.Background()
		ho := hare4.ConsensusOutput{
			Layer:     layerID,
			Proposals: types.ToProposalIDs([]*types.Proposal{p}),
		}
		tg.mockFetch.EXPECT().GetProposals(ctx, ho.Proposals)
		tg.mockPatrol.EXPECT().CompleteHare(layerID)
		got, err := tg.processHareOutput(ctx, ho)
		require.ErrorIs(t, err, errProposalTxHdrMissing)
		require.Nil(t, got)
	})
}

func Test_processHareOutput_EmptyProposals(t *testing.T) {
	tg := createTestGenerator(t)
	numProposals := 10
	lid := types.GetEffectiveGenesis().Add(20)
	signers, atxes := createATXs(t, tg.atxs, (lid.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	plist := make([]*types.Proposal, 0, numProposals)
	for i := 0; i < numProposals; i++ {
		p := createProposal(t, tg.db, tg.proposals, activeSet, lid, types.Hash32{}, activeSet[i], signers[i], nil, 1)
		plist = append(plist, p)
	}
	ctx := context.Background()
	ho := hare4.ConsensusOutput{
		Layer:     lid,
		Proposals: types.ToProposalIDs(plist),
	}
	tg.mockFetch.EXPECT().GetProposals(ctx, ho.Proposals)
	var block *types.Block
	tg.mockMesh.EXPECT().AddBlockWithTXs(ctx, gomock.Any()).DoAndReturn(
		func(_ context.Context, b *types.Block) error {
			block = b
			return nil
		})
	tg.mockCert.EXPECT().RegisterForCert(ctx, lid, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, bid types.BlockID) error {
			require.Equal(t, block.ID(), bid)
			return nil
		})
	tg.mockCert.EXPECT().CertifyIfEligible(ctx, lid, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, bid types.BlockID) error {
			require.Equal(t, block.ID(), bid)
			return eligibility.ErrNotActive
		})
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lid, gomock.Any(), false).DoAndReturn(
		func(_ context.Context, _ types.LayerID, bid types.BlockID, _ bool) error {
			require.Equal(t, block.ID(), bid)
			return nil
		})
	tg.mockPatrol.EXPECT().CompleteHare(lid)
	got, err := tg.processHareOutput(ctx, ho)
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
	txIDs := createAndSaveTxs(t, numTXs, tg.db)
	signers, atxes := createATXs(t, tg.atxs, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	plist := createProposals(t, tg.db, tg.proposals, layerID, types.Hash32{}, signers, activeSet, txIDs)
	ctx := context.Background()
	ho1 := hare4.ConsensusOutput{
		Layer:     layerID,
		Proposals: types.ToProposalIDs(plist),
	}
	tg.mockFetch.EXPECT().GetProposals(ctx, ho1.Proposals)
	tg.mockMesh.EXPECT().AddBlockWithTXs(ctx, gomock.Any())
	tg.mockCert.EXPECT().RegisterForCert(ctx, layerID, gomock.Any())
	tg.mockCert.EXPECT().CertifyIfEligible(ctx, layerID, gomock.Any()).Return(eligibility.ErrNotActive)
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, gomock.Any(), false)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	got1, err := tg.processHareOutput(ctx, ho1)
	require.NoError(t, err)
	require.NotNil(t, got1)
	require.Equal(t, layerID, got1.LayerIndex)
	require.Len(t, got1.TxIDs, numTXs)

	// reorder the proposals
	ordered := plist[numProposals/2 : numProposals]
	ordered = append(ordered, plist[0:numProposals/2]...)
	require.NotEqual(t, plist, ordered)
	ho2 := hare4.ConsensusOutput{
		Layer:     layerID,
		Proposals: types.ToProposalIDs(ordered),
	}
	tg.mockFetch.EXPECT().GetProposals(ctx, ho2.Proposals)
	tg.mockMesh.EXPECT().AddBlockWithTXs(ctx, gomock.Any())
	tg.mockCert.EXPECT().RegisterForCert(ctx, layerID, gomock.Any())
	tg.mockCert.EXPECT().CertifyIfEligible(ctx, layerID, gomock.Any()).Return(eligibility.ErrNotActive)
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, gomock.Any(), false)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	got2, err := tg.processHareOutput(ctx, ho2)
	require.NoError(t, err)
	require.NotNil(t, got2)
	require.Equal(t, got1, got2)
}

func Test_processHareOutput_SameATX(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	numTXs := 1000
	numProposals := 10
	txIDs := createAndSaveTxs(t, numTXs, tg.db)
	signers, atxes := createATXs(t, tg.atxs, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	atxID := activeSet[0]
	plist := []*types.Proposal{
		createProposal(t, tg.db, tg.proposals, activeSet, layerID, types.Hash32{}, atxID, signers[0], txIDs[0:500], 1),
		createProposal(t, tg.db, tg.proposals, activeSet, layerID, types.Hash32{}, atxID, signers[0], txIDs[400:], 1),
	}
	ho := hare4.ConsensusOutput{
		Layer:     layerID,
		Proposals: types.ToProposalIDs(plist),
	}
	tg.mockFetch.EXPECT().GetProposals(context.Background(), ho.Proposals)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	got, err := tg.processHareOutput(context.Background(), ho)
	require.ErrorIs(t, err, errDuplicateATX)
	require.Nil(t, got)
}

func Test_processHareOutput_EmptyATXID(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	numTXs := 1000
	numProposals := 10
	txIDs := createAndSaveTxs(t, numTXs, tg.db)
	signers, atxes := createATXs(t, tg.atxs, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	plist := createProposals(t, tg.db, tg.proposals, layerID, types.Hash32{}, signers[1:], activeSet[1:], txIDs[1:])
	p := createProposal(t, tg.db, tg.proposals, activeSet, layerID, types.Hash32{}, types.EmptyATXID, signers[0],
		txIDs, 1,
	)
	plist = append(plist, p)
	ho := hare4.ConsensusOutput{
		Layer:     layerID,
		Proposals: types.ToProposalIDs(plist),
	}
	tg.mockFetch.EXPECT().GetProposals(context.Background(), ho.Proposals)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	got, err := tg.processHareOutput(context.Background(), ho)
	require.Error(t, err)
	require.Nil(t, got)
}

func Test_processHareOutput_MultipleEligibilities(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	ids := createAndSaveTxs(t, 1000, tg.db)
	signers, atxes := createATXs(t, tg.atxs, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	plist := []*types.Proposal{
		createProposal(t, tg.db, tg.proposals, activeSet, layerID, types.Hash32{}, atxes[0].ID(), signers[0], ids, 2),
		createProposal(t, tg.db, tg.proposals, activeSet, layerID, types.Hash32{}, atxes[1].ID(), signers[1], ids, 1),
		createProposal(t, tg.db, tg.proposals, activeSet, layerID, types.Hash32{}, atxes[2].ID(), signers[2], ids, 5),
	}
	ctx := context.Background()
	ho := hare4.ConsensusOutput{
		Layer:     layerID,
		Proposals: types.ToProposalIDs(plist),
	}
	tg.mockFetch.EXPECT().GetProposals(ctx, ho.Proposals)
	tg.mockMesh.EXPECT().AddBlockWithTXs(ctx, gomock.Any())
	tg.mockCert.EXPECT().RegisterForCert(ctx, layerID, gomock.Any())
	tg.mockCert.EXPECT().CertifyIfEligible(ctx, layerID, gomock.Any()).Return(eligibility.ErrNotActive)
	tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, gomock.Any(), false)
	tg.mockPatrol.EXPECT().CompleteHare(layerID)
	got, err := tg.processHareOutput(ctx, ho)
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
