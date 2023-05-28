package blocks

import (
	"context"
	"errors"
	"math/rand"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

type testHandler struct {
	*Handler
	mockFetcher *smocks.MockFetcher
	mockMesh    *mocks.MockmeshProvider
}

func createTestHandler(t *testing.T) *testHandler {
	ctrl := gomock.NewController(t)
	th := &testHandler{
		mockFetcher: smocks.NewMockFetcher(ctrl),
		mockMesh:    mocks.NewMockmeshProvider(ctrl),
	}
	th.Handler = NewHandler(th.mockFetcher, sql.InMemory(), th.mockMesh, WithLogger(logtest.New(t)))
	return th
}

func createBlockData(t *testing.T, layerID types.LayerID, txIDs []types.TransactionID) (*types.Block, []byte) {
	t.Helper()
	block := &types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: layerID,
			TxIDs:      txIDs,
			Rewards: []types.AnyReward{
				{Weight: types.RatNum{Num: 1, Denom: 1}},
			},
		},
	}
	block.Initialize()
	data, err := codec.Encode(block)
	require.NoError(t, err)
	return block, data
}

func Test_HandleBlockData_MalformedData(t *testing.T) {
	th := createTestHandler(t)
	assert.ErrorIs(t, th.HandleSyncedBlock(context.TODO(), p2p.NoPeer, []byte("malformed")), errMalformedData)
}

func Test_HandleBlockData_InvalidRewards(t *testing.T) {
	th := createTestHandler(t)
	buf, err := codec.Encode(&types.Block{InnerBlock: types.InnerBlock{LayerIndex: 11}})
	require.NoError(t, err)
	require.ErrorIs(t, th.HandleSyncedBlock(context.TODO(), p2p.NoPeer, buf), errInvalidRewards)
}

func Test_HandleBlockData_AlreadyHasBlock(t *testing.T) {
	th := createTestHandler(t)
	layerID := types.LayerID(99)
	txIDs := createTransactions(t, max(10, rand.Intn(100)))

	block, data := createBlockData(t, layerID, txIDs)
	require.NoError(t, blocks.Add(th.db, block))
	assert.NoError(t, th.HandleSyncedBlock(context.TODO(), p2p.NoPeer, data))
}

func Test_HandleBlockData_FailedToFetchTXs(t *testing.T) {
	th := createTestHandler(t)
	layerID := types.LayerID(99)
	txIDs := createTransactions(t, max(10, rand.Intn(100)))

	block, data := createBlockData(t, layerID, txIDs)
	errUnknown := errors.New("unknown")
	th.mockFetcher.EXPECT().GetBlockTxs(gomock.Any(), txIDs).Return(errUnknown).Times(1)
	peer := p2p.Peer("buddy")
	th.mockFetcher.EXPECT().RegisterPeerHashes(peer, types.TransactionIDsToHashes(block.TxIDs))
	assert.ErrorIs(t, th.HandleSyncedBlock(context.TODO(), peer, data), errUnknown)
}

func Test_HandleBlockData_FailedToAddBlock(t *testing.T) {
	th := createTestHandler(t)
	layerID := types.LayerID(99)
	txIDs := createTransactions(t, max(10, rand.Intn(100)))

	block, data := createBlockData(t, layerID, txIDs)
	th.mockFetcher.EXPECT().GetBlockTxs(gomock.Any(), txIDs).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	th.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), block).Return(errUnknown).Times(1)
	peer := p2p.Peer("buddy")
	th.mockFetcher.EXPECT().RegisterPeerHashes(peer, types.TransactionIDsToHashes(block.TxIDs))
	assert.ErrorIs(t, th.HandleSyncedBlock(context.TODO(), peer, data), errUnknown)
}

func Test_HandleBlockData(t *testing.T) {
	th := createTestHandler(t)
	layerID := types.LayerID(99)
	txIDs := createTransactions(t, max(10, rand.Intn(100)))

	block, data := createBlockData(t, layerID, txIDs)
	th.mockFetcher.EXPECT().GetBlockTxs(gomock.Any(), txIDs).Return(nil).Times(1)
	th.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), block).Return(nil).Times(1)
	peer := p2p.Peer("buddy")
	th.mockFetcher.EXPECT().RegisterPeerHashes(peer, types.TransactionIDsToHashes(block.TxIDs))
	assert.NoError(t, th.HandleSyncedBlock(context.TODO(), peer, data))
}

func max(i, j int) int {
	if i > j {
		return i
	}
	return j
}

func TestValidateRewards(t *testing.T) {
	for _, tc := range []struct {
		desc    string
		rewards []types.AnyReward
		err     bool
	}{
		{
			desc: "sanity",
			rewards: []types.AnyReward{
				{
					AtxID:  types.ATXID{1},
					Weight: types.RatNum{Num: 1, Denom: 3},
				},
				{
					AtxID:  types.ATXID{2},
					Weight: types.RatNum{Num: 1, Denom: 3},
				},
				{
					AtxID:  types.ATXID{3},
					Weight: types.RatNum{Num: 1, Denom: 3},
				},
			},
		},
		{
			desc:    "empty",
			rewards: []types.AnyReward{},
			err:     true,
		},
		{
			desc: "nil",
			err:  true,
		},
		{
			desc: "zero num",
			rewards: []types.AnyReward{
				{
					AtxID:  types.ATXID{1},
					Weight: types.RatNum{Num: 1, Denom: 3},
				},
				{
					AtxID:  types.ATXID{3},
					Weight: types.RatNum{Num: 0, Denom: 3},
				},
			},
			err: true,
		},
		{
			desc: "zero denom",
			rewards: []types.AnyReward{
				{
					AtxID:  types.ATXID{1},
					Weight: types.RatNum{Num: 1, Denom: 3},
				},
				{
					AtxID:  types.ATXID{3},
					Weight: types.RatNum{Num: 1, Denom: 0},
				},
			},
			err: true,
		},
		{
			desc: "multiple per coinbase",
			rewards: []types.AnyReward{
				{
					AtxID:  types.ATXID{1},
					Weight: types.RatNum{Num: 1, Denom: 3},
				},
				{
					AtxID:  types.ATXID{3},
					Weight: types.RatNum{Num: 1, Denom: 3},
				},
				{
					AtxID:  types.ATXID{1},
					Weight: types.RatNum{Num: 1, Denom: 3},
				},
			},
			err: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			err := ValidateRewards(tc.rewards)
			if tc.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
