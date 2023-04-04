package blocks

import (
	"bytes"
	"context"
	"errors"
	"math"
	"math/big"
	"math/rand"
	"sort"
	"sync"
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
			types.NIPostChallenge{PubLayerID: lid},
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
	activeSet []types.ATXID,
	txIDs []types.TransactionID,
) []*types.Proposal {
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
		p := createProposal(t, epochData, layerID, meshHash, activeSet[i], signers[i], txIDs[from:to], 1)
		plist = append(plist, p)
		require.NoError(t, proposals.Add(db, p))
		require.NoError(t, ballots.Add(db, &p.Ballot))
	}
	return plist
}

func createProposal(
	t *testing.T,
	epochData *types.EpochData,
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
				BallotMetadata: types.BallotMetadata{
					Layer: lid,
				},
				InnerBallot: types.InnerBallot{
					AtxID:     atxID,
					EpochData: epochData,
				},
				EligibilityProofs: make([]types.VotingEligibility, numEligibility),
			},
			TxIDs:    txIDs,
			MeshHash: meshHash,
		},
	}
	p.Ballot.Signature = signer.Sign(signing.BALLOT, p.Ballot.SignedBytes())
	p.Signature = signer.Sign(signing.BALLOT, p.SignedBytes())
	p.SetSmesherID(signer.NodeID())
	require.NoError(t, p.Initialize())
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

func Test_SerialProcessing(t *testing.T) {
	tg := createTestGenerator(t)
	tg.Start()
	defer tg.Stop()

	numLayers := 4
	var wg sync.WaitGroup
	wg.Add(numLayers)
	for i := uint32(1); i <= uint32(numLayers); i++ {
		lid := types.LayerID(i)
		tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), lid, types.EmptyBlockID).Return(nil)
		tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), lid, types.EmptyBlockID).Return(nil)
		tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), lid, types.EmptyBlockID, false).Do(
			func(_ context.Context, gotL types.LayerID, gotB types.BlockID, _ bool) error {
				require.NoError(t, layers.SetApplied(tg.cdb, gotL, gotB))
				wg.Done()
				return nil
			})
		tg.mockPatrol.EXPECT().CompleteHare(lid)
	}

	tg.hareCh <- hare.LayerOutput{
		Ctx:   context.Background(),
		Layer: types.LayerID(3),
	}
	tg.hareCh <- hare.LayerOutput{
		Ctx:   context.Background(),
		Layer: types.LayerID(4),
	}
	tg.hareCh <- hare.LayerOutput{
		Ctx:   context.Background(),
		Layer: types.LayerID(2),
	}
	tg.hareCh <- hare.LayerOutput{
		Ctx:   context.Background(),
		Layer: types.LayerID(1),
	}

	wg.Wait()
}

func Test_processHareOutput(t *testing.T) {
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
			tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids).Return(nil)

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
			require.NoError(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}))
		})
	}
}

func Test_processHareOutput_corner_cases(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)

	t.Run("empty output", func(t *testing.T) {
		tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, types.EmptyBlockID).Return(nil)
		tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), layerID, types.EmptyBlockID).Return(nil)
		tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, types.EmptyBlockID, false).Return(nil)
		require.NoError(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.Background(), Layer: layerID}))
	})

	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs := createAndSaveTxs(t, numTXs, tg.cdb)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	meshHash := types.RandomHash()
	plist := createProposals(t, tg.cdb, layerID, meshHash, signers, activeSet, txIDs)
	pids := types.ToProposalIDs(plist)

	errUnknown := errors.New("unknown")
	t.Run("fetch failed", func(t *testing.T) {
		tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids).Return(errUnknown)
		require.ErrorIs(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}), errUnknown)
	})

	tg.mockFetch.EXPECT().GetProposals(gomock.Any(), pids).Return(nil).AnyTimes()
	t.Run("node mesh differ from consensus", func(t *testing.T) {
		require.NoError(t, layers.SetMeshHash(tg.cdb, layerID.Sub(1), types.RandomHash()))
		require.ErrorIs(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}), errNodeHasBadMeshHash)
	})

	t.Run("execute failed", func(t *testing.T) {
		require.NoError(t, layers.SetMeshHash(tg.cdb, layerID.Sub(1), meshHash))
		tg.mockExec.EXPECT().ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, lid types.LayerID, tickHeight uint64, rewards []types.AnyReward, tids []types.TransactionID) (*types.Block, error) {
				require.Len(t, tids, len(txIDs))
				return nil, errUnknown
			})
		require.ErrorIs(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}), errUnknown)
	})

	block := types.NewExistingBlock(types.BlockID{1, 2, 3}, types.InnerBlock{LayerIndex: layerID})
	t.Run("failed to add block", func(t *testing.T) {
		tg.mockExec.EXPECT().ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).Return(block, nil)
		tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any()).Return(errUnknown)
		require.ErrorIs(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}), errUnknown)
	})

	t.Run("failure to register cert ignored", func(t *testing.T) {
		tg.mockExec.EXPECT().ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).Return(block, nil)
		tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any()).Return(nil)
		tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, gomock.Any()).Return(errUnknown)
		tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), layerID, gomock.Any()).Return(nil)
		tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, block.ID(), true).Return(nil)
		require.NoError(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}))
	})

	t.Run("failure to certify ignored", func(t *testing.T) {
		tg.mockExec.EXPECT().ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).Return(block, nil)
		tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), gomock.Any()).Return(nil)
		tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, gomock.Any()).Return(nil)
		tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), layerID, gomock.Any()).Return(errUnknown)
		tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, block.ID(), true).Return(nil)
		require.NoError(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}))
	})

	t.Run("mesh failed to process", func(t *testing.T) {
		tg.mockExec.EXPECT().ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).Return(block, nil)
		tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), block).Return(nil)
		tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, block.ID()).Return(nil)
		tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), layerID, block.ID()).Return(nil)
		tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, block.ID(), true).Return(errUnknown)
		require.ErrorIs(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}), errUnknown)
	})

	t.Run("all good", func(t *testing.T) {
		tg.mockExec.EXPECT().ExecuteOptimistic(gomock.Any(), layerID, uint64(baseTickHeight), gomock.Any(), gomock.Any()).Return(block, nil)
		tg.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), block).Return(nil)
		tg.mockCert.EXPECT().RegisterForCert(gomock.Any(), layerID, block.ID()).Return(nil)
		tg.mockCert.EXPECT().CertifyIfEligible(gomock.Any(), gomock.Any(), layerID, block.ID()).Return(nil)
		tg.mockMesh.EXPECT().ProcessLayerPerHareOutput(gomock.Any(), layerID, block.ID(), true).Return(nil)
		require.NoError(t, tg.processHareOutput(hare.LayerOutput{Ctx: context.Background(), Layer: layerID, Proposals: pids}))
	})
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
	pList := createProposals(t, tg.cdb, layerID, types.Hash32{}, signers, activeSet, nil)
	block, executed, err := tg.generateBlock(context.Background(), tg.logger, layerID, pList)
	require.NoError(t, err)
	require.False(t, executed)
	require.Equal(t, max, block.TickHeight)
	require.Len(t, block.Rewards, numProposals)
	// numUnit is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1 eligibility
	// the expected weight for each eligibility is `numUnit` * 1/3
	expWeight := new(big.Rat).SetInt64(numUnit * 1 / 3)
	checkRewards(t, atxes, expWeight, block.Rewards)
}

func Test_generateBlock_bad_state(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), 1)
	activeSet := types.ToATXIDs(atxes)
	epochData := &types.EpochData{
		ActiveSet: activeSet,
		Beacon:    types.RandomBeacon(),
	}

	t.Run("tx missing", func(t *testing.T) {
		p := createProposal(t, epochData, layerID, types.Hash32{}, activeSet[0], signers[0], []types.TransactionID{types.RandomTransactionID()}, 1)
		block, executed, err := tg.generateBlock(context.Background(), tg.logger, layerID, []*types.Proposal{p})
		require.ErrorIs(t, err, errProposalTxMissing)
		require.False(t, executed)
		require.Nil(t, block)
	})

	t.Run("tx header missing", func(t *testing.T) {
		tx := types.Transaction{
			RawTx: types.NewRawTx([]byte{1, 1, 1}),
		}
		require.NoError(t, transactions.Add(tg.cdb, &tx, time.Now()))
		p := createProposal(t, epochData, layerID, types.Hash32{}, activeSet[0], signers[0], []types.TransactionID{tx.ID}, 1)
		block, executed, err := tg.generateBlock(context.Background(), tg.logger, layerID, []*types.Proposal{p})
		require.ErrorIs(t, err, errProposalTxHdrMissing)
		require.False(t, executed)
		require.Nil(t, block)
	})
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
		p := createProposal(t, epochData, lid, types.Hash32{}, activeSet[i], signers[i], nil, 1)
		plist = append(plist, p)
	}
	block, executed, err := tg.generateBlock(context.Background(), tg.logger, lid, plist)
	require.NoError(t, err)
	require.False(t, executed)
	require.NotEqual(t, types.EmptyBlockID, block.ID())

	require.Equal(t, lid, block.LayerIndex)
	require.Empty(t, block.TxIDs)
	require.Len(t, block.Rewards, numProposals)
	// numUint is the ATX weight. eligible slots per epoch is 3 for each atx, each proposal has 1 eligibility
	// the expected weight for each eligibility is `numUnit` * 1/3
	expWeight := new(big.Rat).SetInt64(numUnit * 1 / 3)
	checkRewards(t, atxes, expWeight, block.Rewards)
}

func Test_generateBlock_StableBlockID(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs := createAndSaveTxs(t, numTXs, tg.cdb)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	plist := createProposals(t, tg.cdb, layerID, types.Hash32{}, signers, activeSet, txIDs)
	block, executed, err := tg.generateBlock(context.Background(), tg.logger, layerID, plist)
	require.NoError(t, err)
	require.False(t, executed)
	require.NotEqual(t, types.EmptyBlockID, block.ID())
	require.Equal(t, layerID, block.LayerIndex)
	require.Len(t, block.TxIDs, numTXs)

	// reorder the proposals
	ordered := plist[numProposals/2 : numProposals]
	ordered = append(ordered, plist[0:numProposals/2]...)
	require.NotEqual(t, plist, ordered)
	block2, executed, err := tg.generateBlock(context.Background(), tg.logger, layerID, ordered)
	require.NoError(t, err)
	require.False(t, executed)
	require.Equal(t, block, block2)
	require.Equal(t, block.ID(), block2.ID())
}

func Test_generateBlock_SameATX(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	numTXs := 1000
	numProposals := 10
	txIDs := createAndSaveTxs(t, numTXs, tg.cdb)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	epochData := &types.EpochData{
		ActiveSet: activeSet,
		Beacon:    types.RandomBeacon(),
	}
	atxID := activeSet[0]
	proposal1 := createProposal(t, epochData, layerID, types.Hash32{}, atxID, signers[0], txIDs[0:500], 1)
	proposal2 := createProposal(t, epochData, layerID, types.Hash32{}, atxID, signers[0], txIDs[400:], 1)
	plist := []*types.Proposal{proposal1, proposal2}
	block, executed, err := tg.generateBlock(context.Background(), tg.logger, layerID, plist)
	require.ErrorIs(t, err, errDuplicateATX)
	require.False(t, executed)
	require.Nil(t, block)
}

func Test_generateBlock_EmptyATXID(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	// create multiple proposals with overlapping TXs
	numTXs := 1000
	numProposals := 10
	txIDs := createAndSaveTxs(t, numTXs, tg.cdb)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), numProposals)
	activeSet := types.ToATXIDs(atxes)
	plist := createProposals(t, tg.cdb, layerID, types.Hash32{}, signers, activeSet, txIDs)
	// set the last proposal ID to be empty
	types.SortProposals(plist)
	plist[numProposals-1].AtxID = types.EmptyATXID
	block, executed, err := tg.generateBlock(context.Background(), tg.logger, layerID, plist)
	require.ErrorIs(t, err, errInvalidATXID)
	require.False(t, executed)
	require.Nil(t, block)
}

func Test_generateBlock_MultipleEligibilities(t *testing.T) {
	tg := createTestGenerator(t)
	layerID := types.GetEffectiveGenesis().Add(100)
	ids := createAndSaveTxs(t, 1000, tg.cdb)
	signers, atxes := createATXs(t, tg.cdb, (layerID.GetEpoch() - 1).FirstLayer(), 10)
	activeSet := types.ToATXIDs(atxes)
	epochData := &types.EpochData{
		ActiveSet: activeSet,
		Beacon:    types.RandomBeacon(),
	}
	plist := []*types.Proposal{
		createProposal(t, epochData, layerID, types.Hash32{}, atxes[0].ID(), signers[0], ids, 2),
		createProposal(t, epochData, layerID, types.Hash32{}, atxes[1].ID(), signers[1], ids, 1),
		createProposal(t, epochData, layerID, types.Hash32{}, atxes[2].ID(), signers[2], ids, 5),
	}

	block, executed, err := tg.generateBlock(context.Background(), tg.logger, layerID, plist)
	require.NoError(t, err)
	require.False(t, executed)
	require.Len(t, block.Rewards, len(plist))
	sort.Slice(plist, func(i, j int) bool {
		return bytes.Compare(plist[i].AtxID.Bytes(), plist[j].AtxID.Bytes()) < 0
	})
	totalWeight := new(big.Rat)
	for i, r := range block.Rewards {
		require.Equal(t, plist[i].AtxID, r.AtxID)
		got := r.Weight.ToBigRat()
		// numUint is the ATX weight. eligible slots per epoch is 3 for each atx
		// the expected weight for each eligibility is `numUnit` * 1/3
		require.Equal(t, new(big.Rat).SetInt64(numUnit*1/3*int64(len(plist[i].EligibilityProofs))), got)
		totalWeight.Add(totalWeight, got)
	}
	require.Equal(t, new(big.Rat).SetInt64(numUnit*8*1/3), totalWeight)
}
