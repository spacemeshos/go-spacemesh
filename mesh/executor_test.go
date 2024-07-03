package mesh_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/mesh/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(4)
	res := m.Run()
	os.Exit(res)
}

type testExecutor struct {
	tb       testing.TB
	exec     *mesh.Executor
	db       *sql.Database
	atxsdata *atxsdata.Data
	mcs      *mocks.MockconservativeState
	mvm      *mocks.MockvmState
}

func newTestExecutor(t *testing.T) *testExecutor {
	ctrl := gomock.NewController(t)
	te := &testExecutor{
		tb:       t,
		db:       sql.InMemory(),
		atxsdata: atxsdata.New(),
		mvm:      mocks.NewMockvmState(ctrl),
		mcs:      mocks.NewMockconservativeState(ctrl),
	}
	lg := logtest.New(t)
	te.exec = mesh.NewExecutor(te.db, te.atxsdata, te.mvm, te.mcs, lg)
	return te
}

func makeResults(lid types.LayerID, txs ...types.Transaction) []types.TransactionWithResult {
	var results []types.TransactionWithResult
	for _, tx := range txs {
		results = append(results, types.TransactionWithResult{
			Transaction: tx,
			TransactionResult: types.TransactionResult{
				Layer: lid,
			},
		})
	}
	return results
}

func (t *testExecutor) createATX(epoch types.EpochID, cb types.Address) (types.ATXID, types.NodeID) {
	sig, err := signing.NewEdSigner()
	require.NoError(t.tb, err)
	atx := types.NewActivationTx(
		types.NIPostChallenge{PublishEpoch: epoch},
		cb,
		11,
	)
	atx.VRFNonce = 1
	atx.SetReceived(time.Now())
	atx.SmesherID = sig.NodeID()
	atx.SetID(types.RandomATXID())
	atx.TickCount = 1
	require.NoError(t.tb, atxs.Add(t.db, atx, types.AtxBlob{}))
	t.atxsdata.AddFromAtx(atx, false)
	return atx.ID(), sig.NodeID()
}

func TestExecutor_Execute(t *testing.T) {
	te := newTestExecutor(t)
	genesis := types.GetEffectiveGenesis()
	lid := genesis
	require.NoError(t, layers.SetApplied(te.db, lid, types.EmptyBlockID))

	t.Run("layer already applied", func(t *testing.T) {
		require.ErrorIs(t, te.exec.Execute(context.Background(), lid, nil), mesh.ErrLayerApplied)
	})

	t.Run("layer out of order", func(t *testing.T) {
		require.ErrorIs(t, te.exec.Execute(context.Background(), lid.Add(2), nil), mesh.ErrLayerNotInOrder)
	})

	lid = lid.Add(1)
	t.Run("txs missing", func(t *testing.T) {
		block := types.NewExistingBlock(types.RandomBlockID(), types.InnerBlock{
			LayerIndex: lid,
			TxIDs:      types.RandomTXSet(10),
		})
		require.ErrorIs(t, te.exec.Execute(context.Background(), block.LayerIndex, block), sql.ErrNotFound)
	})

	t.Run("empty layer", func(t *testing.T) {
		te.mvm.EXPECT().Apply(vm.ApplyContext{Layer: lid}, nil, nil)
		te.mcs.EXPECT().UpdateCache(gomock.Any(), lid, types.EmptyBlockID, nil, nil)
		te.mvm.EXPECT().GetStateRoot()
		require.NoError(t, te.exec.Execute(context.Background(), lid, nil))
		require.NoError(t, layers.SetApplied(te.db, lid, types.EmptyBlockID))
	})

	lid = lid.Add(1)
	block := types.NewExistingBlock(types.RandomBlockID(), types.InnerBlock{
		LayerIndex: lid,
	})
	t.Run("empty block", func(t *testing.T) {
		te.mvm.EXPECT().Apply(vm.ApplyContext{Layer: lid}, []types.Transaction{}, []types.CoinbaseReward{})
		te.mcs.EXPECT().UpdateCache(gomock.Any(), lid, block.ID(), nil, nil)
		te.mvm.EXPECT().GetStateRoot()
		require.NoError(t, te.exec.Execute(context.Background(), block.LayerIndex, block))
		require.NoError(t, layers.SetApplied(te.db, lid, block.ID()))
	})

	lid = lid.Add(1)
	cbs := []types.Address{{1, 2, 3}, {2, 3, 4}}
	atxid1, nodeId1 := te.createATX(genesis.GetEpoch(), cbs[0])
	atxid2, nodeId2 := te.createATX(genesis.GetEpoch(), cbs[1])
	rewards := []types.AnyReward{
		{
			AtxID:  atxid1,
			Weight: types.RatNum{Num: 1, Denom: 3},
		},
		{
			AtxID:  atxid2,
			Weight: types.RatNum{Num: 2, Denom: 3},
		},
	}
	expRewards := []types.CoinbaseReward{
		{
			SmesherID: nodeId1,
			Coinbase:  cbs[0],
			Weight:    rewards[0].Weight,
		},
		{
			SmesherID: nodeId2,
			Coinbase:  cbs[1],
			Weight:    rewards[1].Weight,
		},
	}
	sort.Slice(expRewards, func(i, j int) bool {
		return bytes.Compare(expRewards[i].Coinbase.Bytes(), expRewards[j].Coinbase.Bytes()) < 0
	})

	block = types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{
		LayerIndex: lid,
		TxIDs:      mesh.CreateAndSaveTxs(t, te.db, 10),
		Rewards:    rewards,
	})
	errTest := errors.New("test")
	t.Run("vm failure", func(t *testing.T) {
		te.mvm.EXPECT().Apply(vm.ApplyContext{Layer: block.LayerIndex}, gomock.Any(), expRewards).
			DoAndReturn(func(
				_ vm.ApplyContext,
				gotTxs []types.Transaction,
				_ []types.CoinbaseReward,
			) ([]types.Transaction, []types.TransactionWithResult, error) {
				tids := make([]types.TransactionID, 0, len(gotTxs))
				for _, tx := range gotTxs {
					tids = append(tids, tx.ID)
				}
				require.Equal(t, block.TxIDs, tids)
				return nil, nil, errTest
			})
		require.ErrorIs(t, te.exec.Execute(context.Background(), block.LayerIndex, block), errTest)
	})

	var executed []types.TransactionWithResult
	var ineffective []types.Transaction
	t.Run("conservative cache failure", func(t *testing.T) {
		te.mvm.EXPECT().Apply(vm.ApplyContext{Layer: block.LayerIndex}, gomock.Any(), expRewards).DoAndReturn(
			func(
				_ vm.ApplyContext,
				gotTxs []types.Transaction,
				_ []types.CoinbaseReward,
			) ([]types.Transaction, []types.TransactionWithResult, error) {
				tids := make([]types.TransactionID, 0, len(gotTxs))
				for _, tx := range gotTxs {
					tids = append(tids, tx.ID)
				}
				require.Equal(t, block.TxIDs, tids)
				// make first tx ineffective
				ineffective = gotTxs[:1]
				executed = makeResults(block.LayerIndex, gotTxs[1:]...)
				return ineffective, executed, nil
			})
		te.mcs.EXPECT().UpdateCache(gomock.Any(), block.LayerIndex, block.ID(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(
				_ context.Context,
				_ types.LayerID,
				gotBid types.BlockID,
				gotExecuted []types.TransactionWithResult,
				gotIneffective []types.Transaction,
			) error {
				require.Equal(t, executed, gotExecuted)
				require.Equal(t, ineffective, gotIneffective)
				for _, tr := range executed {
					require.Equal(t, tr.Block, gotBid)
				}
				return errTest
			})
		require.ErrorIs(t, te.exec.Execute(context.Background(), block.LayerIndex, block), errTest)
	})

	t.Run("applied block", func(t *testing.T) {
		te.mvm.EXPECT().Apply(vm.ApplyContext{Layer: block.LayerIndex}, gomock.Any(), expRewards).DoAndReturn(
			func(
				_ vm.ApplyContext,
				gotTxs []types.Transaction,
				_ []types.CoinbaseReward,
			) ([]types.Transaction, []types.TransactionWithResult, error) {
				tids := make([]types.TransactionID, 0, len(gotTxs))
				for _, tx := range gotTxs {
					tids = append(tids, tx.ID)
				}
				require.Equal(t, block.TxIDs, tids)
				// make first tx ineffective
				ineffective = gotTxs[:1]
				executed = makeResults(block.LayerIndex, gotTxs[1:]...)
				return ineffective, executed, nil
			})
		te.mcs.EXPECT().UpdateCache(gomock.Any(), block.LayerIndex, block.ID(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(
				_ context.Context,
				_ types.LayerID, gotBid types.BlockID,
				gotExecuted []types.TransactionWithResult,
				gotIneffective []types.Transaction,
			) error {
				require.Equal(t, executed, gotExecuted)
				require.Equal(t, ineffective, gotIneffective)
				for _, tr := range gotExecuted {
					require.Equal(t, tr.Block, gotBid)
				}
				return nil
			})
		te.mvm.EXPECT().GetStateRoot()
		require.NoError(t, te.exec.Execute(context.Background(), block.LayerIndex, block))
		require.NoError(t, layers.SetApplied(te.db, lid, block.ID()))
	})
}

func TestExecutor_ExecuteOptimistic(t *testing.T) {
	te := newTestExecutor(t)
	lid := types.GetEffectiveGenesis()
	tickHeight := uint64(111)
	cbs := []types.Address{{1, 2, 3}, {2, 3, 4}}
	atxId1, nodeId1 := te.createATX(lid.GetEpoch(), cbs[0])
	atxId2, nodeId2 := te.createATX(lid.GetEpoch(), cbs[1])
	rewards := []types.AnyReward{
		{
			AtxID:  atxId1,
			Weight: types.RatNum{Num: 1, Denom: 3},
		},
		{
			AtxID:  atxId2,
			Weight: types.RatNum{Num: 2, Denom: 3},
		},
	}
	expRewards := []types.CoinbaseReward{
		{
			SmesherID: nodeId1,
			Coinbase:  cbs[0],
			Weight:    rewards[0].Weight,
		},
		{
			SmesherID: nodeId2,
			Coinbase:  cbs[1],
			Weight:    rewards[1].Weight,
		},
	}
	sort.Slice(expRewards, func(i, j int) bool {
		return bytes.Compare(expRewards[i].Coinbase.Bytes(), expRewards[j].Coinbase.Bytes()) < 0
	})

	tids := mesh.CreateAndSaveTxs(t, te.db, 10)
	require.NoError(t, layers.SetApplied(te.db, lid, types.EmptyBlockID))

	t.Run("layer already applied", func(t *testing.T) {
		block, err := te.exec.ExecuteOptimistic(context.Background(), lid, tickHeight, rewards, tids)
		require.ErrorIs(t, err, mesh.ErrLayerApplied)
		require.Nil(t, block)
	})

	t.Run("layer out of order", func(t *testing.T) {
		block, err := te.exec.ExecuteOptimistic(context.Background(), lid.Add(2), tickHeight, rewards, tids)
		require.ErrorIs(t, err, mesh.ErrLayerNotInOrder)
		require.Nil(t, block)
	})

	lid = lid.Add(1)
	t.Run("txs missing", func(t *testing.T) {
		block, err := te.exec.ExecuteOptimistic(context.Background(), lid, tickHeight, rewards, types.RandomTXSet(100))
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, block)
	})

	errTest := errors.New("test")
	t.Run("vm failure", func(t *testing.T) {
		te.mvm.EXPECT().Apply(vm.ApplyContext{Layer: lid}, gomock.Any(), expRewards).DoAndReturn(
			func(
				_ vm.ApplyContext,
				gotTxs []types.Transaction,
				_ []types.CoinbaseReward,
			) ([]types.Transaction, []types.TransactionWithResult, error) {
				gotTids := make([]types.TransactionID, 0, len(gotTxs))
				for _, tx := range gotTxs {
					gotTids = append(gotTids, tx.ID)
				}
				require.Equal(t, tids, gotTids)
				return nil, nil, errTest
			})
		block, err := te.exec.ExecuteOptimistic(context.Background(), lid, tickHeight, rewards, tids)
		require.ErrorIs(t, err, errTest)
		require.Nil(t, block)
	})

	var executed []types.TransactionWithResult
	var ineffective []types.Transaction
	t.Run("conservative cache failure", func(t *testing.T) {
		te.mvm.EXPECT().Apply(vm.ApplyContext{Layer: lid}, gomock.Any(), expRewards).DoAndReturn(
			func(
				_ vm.ApplyContext,
				gotTxs []types.Transaction,
				_ []types.CoinbaseReward,
			) ([]types.Transaction, []types.TransactionWithResult, error) {
				gotTids := make([]types.TransactionID, 0, len(gotTxs))
				for _, tx := range gotTxs {
					gotTids = append(gotTids, tx.ID)
				}
				require.Equal(t, tids, gotTids)
				// make first tx ineffective
				ineffective = gotTxs[:1]
				executed = makeResults(lid, gotTxs[1:]...)
				return ineffective, executed, nil
			})
		expBlock := &types.Block{
			InnerBlock: types.InnerBlock{
				LayerIndex: lid,
				TickHeight: tickHeight,
				Rewards:    rewards,
				TxIDs:      tids[1:],
			},
		}
		expBlock.Initialize()
		te.mcs.EXPECT().UpdateCache(gomock.Any(), lid, expBlock.ID(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(
				_ context.Context,
				_ types.LayerID,
				gotBid types.BlockID,
				gotExecuted []types.TransactionWithResult,
				gotIneffective []types.Transaction,
			) error {
				require.Equal(t, executed, gotExecuted)
				require.Equal(t, ineffective, gotIneffective)
				for _, tr := range gotExecuted {
					require.Equal(t, tr.Block, gotBid)
				}
				return errTest
			})
		block, err := te.exec.ExecuteOptimistic(context.Background(), lid, tickHeight, rewards, tids)
		require.ErrorIs(t, err, errTest)
		require.Nil(t, block)
	})

	t.Run("executed in situ", func(t *testing.T) {
		te.mvm.EXPECT().Apply(vm.ApplyContext{Layer: lid}, gomock.Any(), expRewards).DoAndReturn(
			func(
				_ vm.ApplyContext,
				gotTxs []types.Transaction,
				_ []types.CoinbaseReward,
			) ([]types.Transaction, []types.TransactionWithResult, error) {
				gotTids := make([]types.TransactionID, 0, len(gotTxs))
				for _, tx := range gotTxs {
					gotTids = append(gotTids, tx.ID)
				}
				require.Equal(t, tids, gotTids)
				// make first tx ineffective
				ineffective = gotTxs[:1]
				executed = makeResults(lid, gotTxs[1:]...)
				return ineffective, executed, nil
			})
		expBlock := &types.Block{
			InnerBlock: types.InnerBlock{
				LayerIndex: lid,
				TickHeight: tickHeight,
				Rewards:    rewards,
				TxIDs:      tids[1:],
			},
		}
		expBlock.Initialize()
		te.mcs.EXPECT().
			UpdateCache(gomock.Any(), expBlock.LayerIndex, expBlock.ID(), gomock.Any(), gomock.Any()).
			DoAndReturn(
				func(
					_ context.Context,
					_ types.LayerID,
					gotBid types.BlockID,
					gotExecuted []types.TransactionWithResult,
					gotIneffective []types.Transaction,
				) error {
					require.Equal(t, executed, gotExecuted)
					require.Equal(t, ineffective, gotIneffective)
					for _, tr := range gotExecuted {
						require.Equal(t, tr.Block, gotBid)
					}
					return nil
				})
		te.mvm.EXPECT().GetStateRoot()
		block, err := te.exec.ExecuteOptimistic(context.Background(), lid, tickHeight, rewards, tids)
		require.NoError(t, err)
		require.Equal(t, expBlock, block)
		require.NoError(t, layers.SetApplied(te.db, lid, block.ID()))
	})

	lid = lid.Add(1)
	t.Run("no txs in block", func(t *testing.T) {
		te.mvm.EXPECT().Apply(vm.ApplyContext{Layer: lid}, gomock.Len(0), expRewards)
		expBlock := &types.Block{
			InnerBlock: types.InnerBlock{
				LayerIndex: lid,
				TickHeight: tickHeight,
				Rewards:    rewards,
			},
		}
		expBlock.Initialize()
		te.mcs.EXPECT().UpdateCache(gomock.Any(), lid, expBlock.ID(), nil, nil)
		te.mvm.EXPECT().GetStateRoot()
		block, err := te.exec.ExecuteOptimistic(context.Background(), lid, tickHeight, rewards, nil)
		require.NoError(t, err)
		require.Equal(t, expBlock, block)
		require.NoError(t, layers.SetApplied(te.db, lid, block.ID()))
	})
}

func TestExecutor_Revert(t *testing.T) {
	te := newTestExecutor(t)
	lid := types.GetEffectiveGenesis()
	require.NoError(t, layers.SetApplied(te.db, lid.Add(1), types.RandomBlockID()))

	errInconceivable := errors.New("inconceivable")
	t.Run("vm failure", func(t *testing.T) {
		te.mvm.EXPECT().Revert(lid).Return(errInconceivable)
		require.ErrorIs(t, te.exec.Revert(context.Background(), lid), errInconceivable)
	})

	t.Run("conservative state failure", func(t *testing.T) {
		te.mvm.EXPECT().Revert(lid)
		te.mcs.EXPECT().RevertCache(lid).Return(errInconceivable)
		require.ErrorIs(t, te.exec.Revert(context.Background(), lid), errInconceivable)
	})

	t.Run("revert success", func(t *testing.T) {
		te.mvm.EXPECT().Revert(lid)
		te.mcs.EXPECT().RevertCache(lid)
		te.mvm.EXPECT().GetStateRoot()
		require.NoError(t, te.exec.Revert(context.Background(), lid))
	})
}
