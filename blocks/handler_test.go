package blocks

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/vm/transaction"
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
		},
	}
	block.Initialize()
	data, err := codec.Encode(block)
	require.NoError(t, err)
	return block, data
}

func createAndSaveTxs(t *testing.T, db *sql.Database, numTXs int) []types.TransactionID {
	t.Helper()
	txIDs := make([]types.TransactionID, 0, numTXs)
	for i := 0; i < numTXs; i++ {
		tx, err := transaction.GenerateCallTransaction(signing.NewEdSigner(), types.HexToAddress("1"), 1, 10, 100, 3)
		require.NoError(t, err)
		require.NoError(t, transactions.Add(db, tx, time.Now()))
		txIDs = append(txIDs, tx.ID())
	}
	return txIDs
}

func Test_HandleBlockData_MalformedData(t *testing.T) {
	th := createTestHandler(t)
	layerID := types.NewLayerID(99)
	txIDs := createTransactions(t, max(10, rand.Intn(100)))

	_, data := createBlockData(t, layerID, txIDs)
	assert.ErrorIs(t, th.HandleBlockData(context.TODO(), data[1:]), errMalformedData)
}

func Test_HandleBlockData_AlreadyHasBlock(t *testing.T) {
	th := createTestHandler(t)
	layerID := types.NewLayerID(99)
	txIDs := createTransactions(t, max(10, rand.Intn(100)))

	block, data := createBlockData(t, layerID, txIDs)
	require.NoError(t, blocks.Add(th.db, block))
	assert.NoError(t, th.HandleBlockData(context.TODO(), data))
}

func Test_HandleBlockData_FailedToFetchTXs(t *testing.T) {
	th := createTestHandler(t)
	layerID := types.NewLayerID(99)
	txIDs := createTransactions(t, max(10, rand.Intn(100)))

	_, data := createBlockData(t, layerID, txIDs)
	errUnknown := errors.New("unknown")
	th.mockFetcher.EXPECT().GetTxs(gomock.Any(), txIDs).Return(errUnknown).Times(1)
	assert.ErrorIs(t, th.HandleBlockData(context.TODO(), data), errUnknown)
}

func Test_HandleBlockData_TXsOutOfOrder(t *testing.T) {
	th := createTestHandler(t)
	layerID := types.NewLayerID(99)
	signer := signing.NewEdSigner()
	tx1, err := transaction.GenerateCallTransaction(signer, types.HexToAddress("1"), 1, 10, 100, 3)
	require.NoError(t, err)
	require.NoError(t, transactions.Add(th.db, tx1, time.Now()))
	tx2, err := transaction.GenerateCallTransaction(signer, types.HexToAddress("1"), 2, 10, 100, 3)
	require.NoError(t, err)
	require.NoError(t, transactions.Add(th.db, tx2, time.Now()))
	txIDs := types.ToTransactionIDs([]*types.Transaction{tx2, tx1})

	_, data := createBlockData(t, layerID, txIDs)
	th.mockFetcher.EXPECT().GetTxs(gomock.Any(), txIDs).Return(nil).Times(1)
	assert.ErrorIs(t, th.HandleBlockData(context.TODO(), data), errTXsOutOfOrder)
}

func Test_HandleBlockData_FailedToAddBlock(t *testing.T) {
	th := createTestHandler(t)
	layerID := types.NewLayerID(99)
	txIDs := createAndSaveTxs(t, th.db, max(10, rand.Intn(100)))

	block, data := createBlockData(t, layerID, txIDs)
	th.mockFetcher.EXPECT().GetTxs(gomock.Any(), txIDs).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	th.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), block).Return(errUnknown).Times(1)
	assert.ErrorIs(t, th.HandleBlockData(context.TODO(), data), errUnknown)
}

func Test_HandleBlockData(t *testing.T) {
	th := createTestHandler(t)
	layerID := types.NewLayerID(99)
	txIDs := createAndSaveTxs(t, th.db, max(10, rand.Intn(100)))

	block, data := createBlockData(t, layerID, txIDs)
	th.mockFetcher.EXPECT().GetTxs(gomock.Any(), txIDs).Return(nil).Times(1)
	th.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), block).Return(nil).Times(1)
	assert.NoError(t, th.HandleBlockData(context.TODO(), data))
}

func max(i, j int) int {
	if i > j {
		return i
	}
	return j
}
