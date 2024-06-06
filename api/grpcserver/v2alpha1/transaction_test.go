package v2alpha1

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/fixture"
	"github.com/spacemeshos/go-spacemesh/common/types"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	"github.com/spacemeshos/go-spacemesh/txs"
)

func TestTransactionService_List(t *testing.T) {
	types.SetLayersPerEpoch(5)
	db := sql.InMemory()
	ctx := context.Background()

	gen := fixture.NewTransactionResultGenerator().WithAddresses(2)
	txsList := make([]types.TransactionWithResult, 100)
	require.NoError(t, db.WithTx(ctx, func(dtx *sql.Tx) error {
		for i := range txsList {
			tx := gen.Next()

			require.NoError(t, transactions.Add(dtx, &tx.Transaction, time.Time{}))
			require.NoError(t, transactions.AddResult(dtx, tx.ID, &tx.TransactionResult))
			txsList[i] = *tx
		}
		return nil
	}))

	svc := NewTransactionService(db, nil, nil, nil, nil)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(ctx, t, cfg)
	client := spacemeshv2alpha1.NewTransactionServiceClient(conn)

	t.Run("limit set too high", func(t *testing.T) {
		_, err := client.List(ctx, &spacemeshv2alpha1.TransactionRequest{Limit: 200})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, "limit is capped at 100", s.Message())
	})

	t.Run("no limit set", func(t *testing.T) {
		_, err := client.List(ctx, &spacemeshv2alpha1.TransactionRequest{})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		require.Equal(t, "limit must be set to <= 100", s.Message())
	})

	t.Run("limit and offset", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.TransactionRequest{Limit: 25, Offset: 50})
		require.NoError(t, err)
		require.Len(t, list.Transactions, 25)
	})

	t.Run("all", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.TransactionRequest{Limit: 100})
		require.NoError(t, err)
		require.Len(t, list.Transactions, len(txsList))
	})

	t.Run("principal", func(t *testing.T) {
		principal := txsList[0].TxHeader.Principal.String()
		list, err := client.List(ctx, &spacemeshv2alpha1.TransactionRequest{
			Principal: &principal,
			Limit:     1,
		})
		require.NoError(t, err)
		require.Len(t, list.Transactions, 1)
		require.Equal(t, list.Transactions[0].Tx.Principal, principal)
	})

	t.Run("tx id", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.TransactionRequest{
			Txid:  [][]byte{txsList[0].ID[:]},
			Limit: 100,
		})
		require.NoError(t, err)
		require.Len(t, list.Transactions, 1)
		require.Equal(t, list.Transactions[0].Tx.Id, txsList[0].ID[:])
	})

	t.Run("multiple tx ids", func(t *testing.T) {
		ids := [][]byte{txsList[0].ID[:], txsList[1].ID[:]}
		list, err := client.List(ctx, &spacemeshv2alpha1.TransactionRequest{
			Txid:  ids,
			Limit: 100,
		})
		require.NoError(t, err)
		require.Len(t, list.Transactions, 2)
		require.Contains(t, ids, txsList[0].ID[:])
		require.Contains(t, ids, txsList[1].ID[:])
	})

	t.Run("tx id include result", func(t *testing.T) {
		list, err := client.List(ctx, &spacemeshv2alpha1.TransactionRequest{
			Txid:          [][]byte{txsList[0].ID[:]},
			Limit:         100,
			IncludeResult: true,
			IncludeState:  true,
		})
		require.NoError(t, err)
		require.Len(t, list.Transactions, 1)
		require.Equal(t, txsList[0].ID[:], list.Transactions[0].Tx.Id)
		require.Equal(t, spacemeshv2alpha1.TransactionResult_TRANSACTION_STATUS_SUCCESS,
			list.Transactions[0].TxResult.Status)
	})

	t.Run("start layer & end layer", func(t *testing.T) {
		layer := txsList[0].Layer.Uint32()

		var expectedTxs []types.TransactionWithResult
		for _, tx := range txsList {
			if tx.Layer.Uint32() == layer {
				expectedTxs = append(expectedTxs, tx)
			}
		}

		list, err := client.List(ctx, &spacemeshv2alpha1.TransactionRequest{
			StartLayer: &layer,
			EndLayer:   &layer,
			Limit:      100,
		})
		require.NoError(t, err)
		require.Len(t, list.Transactions, len(expectedTxs))
	})
}

func TestTransactionService_EstimateGas(t *testing.T) {
	types.SetLayersPerEpoch(5)
	db := sql.InMemory()
	vminst := vm.New(db)
	ctx := context.Background()

	svc := NewTransactionService(db, txs.NewConservativeState(vminst, db), nil, nil, nil)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	keys := make([]signing.PrivateKey, 4)
	accounts := make([]types.Account, len(keys))
	rng := rand.New(rand.NewSource(10101))
	for i := range keys {
		pub, priv, err := ed25519.GenerateKey(rng)
		require.NoError(t, err)
		keys[i] = priv
		accounts[i] = types.Account{Address: wallet.Address(pub), Balance: 1e12}
	}
	require.NoError(t, vminst.ApplyGenesis(accounts))
	_, _, err := vminst.Apply(vm.ApplyContext{Layer: types.GetEffectiveGenesis().Add(1)},
		[]types.Transaction{{RawTx: types.NewRawTx(wallet.SelfSpawn(keys[0], 0))}}, nil)
	require.NoError(t, err)

	conn := dialGrpc(ctx, t, cfg)
	client := spacemeshv2alpha1.NewTransactionServiceClient(conn)

	t.Run("valid tx", func(t *testing.T) {
		resp, err := client.EstimateGas(ctx, &spacemeshv2alpha1.EstimateGasRequest{
			Transaction: wallet.Spend(keys[0], accounts[3].Address, 100, 0),
		})
		require.NoError(t, err)
		require.Equal(t, uint64(36090), resp.RecommendedMaxGas)
	})
	t.Run("malformed tx", func(t *testing.T) {
		_, err := client.EstimateGas(ctx, &spacemeshv2alpha1.EstimateGasRequest{
			Transaction: []byte("malformed"),
		})
		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		assert.Contains(t, s.Message(), "malformed tx")
	})
	t.Run("empty", func(t *testing.T) {
		_, err := client.EstimateGas(ctx, &spacemeshv2alpha1.EstimateGasRequest{
			Transaction: nil,
		})
		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		assert.Contains(t, s.Message(), "empty")
	})
	t.Run("not spawned", func(t *testing.T) {
		_, err := client.EstimateGas(ctx, &spacemeshv2alpha1.EstimateGasRequest{
			Transaction: wallet.Spend(keys[2], accounts[3].Address, 100, 0),
		})
		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, s.Code())
		assert.Contains(t, s.Message(), "not spawned")
	})
}

func TestTransactionService_ParseTransaction(t *testing.T) {
	types.SetLayersPerEpoch(5)
	db := sql.InMemory()
	vminst := vm.New(db)
	ctx := context.Background()

	svc := NewTransactionService(db, txs.NewConservativeState(vminst, db), nil, nil, nil)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	keys := make([]signing.PrivateKey, 4)
	accounts := make([]types.Account, len(keys))
	rng := rand.New(rand.NewSource(10101))
	for i := range keys {
		pub, priv, err := ed25519.GenerateKey(rng)
		require.NoError(t, err)
		keys[i] = priv
		accounts[i] = types.Account{Address: wallet.Address(pub), Balance: 1e12}
	}
	require.NoError(t, vminst.ApplyGenesis(accounts))
	_, _, err := vminst.Apply(vm.ApplyContext{Layer: types.GetEffectiveGenesis().Add(1)},
		[]types.Transaction{{RawTx: types.NewRawTx(wallet.SelfSpawn(keys[0], 0))}}, nil)
	require.NoError(t, err)

	mangled := wallet.Spend(keys[0], accounts[3].Address, 100, 0)
	mangled[len(mangled)-1] -= 1

	conn := dialGrpc(ctx, t, cfg)
	client := spacemeshv2alpha1.NewTransactionServiceClient(conn)

	t.Run("valid tx", func(t *testing.T) {
		resp, err := client.ParseTransaction(ctx, &spacemeshv2alpha1.ParseTransactionRequest{
			Transaction: wallet.Spend(keys[0], accounts[3].Address, 100, 0),
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp)
	})
	t.Run("valid tx with verify set to true", func(t *testing.T) {
		resp, err := client.ParseTransaction(ctx, &spacemeshv2alpha1.ParseTransactionRequest{
			Transaction: wallet.Spend(keys[0], accounts[3].Address, 100, 0),
			Verify:      true,
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp)
	})
	t.Run("malformed tx", func(t *testing.T) {
		_, err := client.ParseTransaction(ctx, &spacemeshv2alpha1.ParseTransactionRequest{
			Transaction: []byte("malformed"),
		})
		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		assert.Contains(t, s.Message(), "malformed tx")
	})
	t.Run("empty", func(t *testing.T) {
		_, err := client.ParseTransaction(ctx, &spacemeshv2alpha1.ParseTransactionRequest{
			Transaction: nil,
		})
		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		assert.Contains(t, s.Message(), "empty")
	})
	t.Run("not spawned", func(t *testing.T) {
		_, err := client.ParseTransaction(ctx, &spacemeshv2alpha1.ParseTransactionRequest{
			Transaction: wallet.Spend(keys[2], accounts[3].Address, 100, 0),
		})
		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, s.Code())
		assert.Contains(t, s.Message(), "not spawned")
	})
	t.Run("mangled signature", func(t *testing.T) {
		_, err := client.ParseTransaction(ctx, &spacemeshv2alpha1.ParseTransactionRequest{
			Transaction: mangled,
			Verify:      true,
		})
		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, s.Code())
		assert.Contains(t, s.Message(), "signature is invalid")
	})
}

func TestTransactionServiceSubmitUnsync(t *testing.T) {
	req := require.New(t)

	ctrl := gomock.NewController(t)
	syncer := NewMocktransactionSyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(false)
	publisher := pubsubmocks.NewMockPublisher(ctrl)
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	txHandler := NewMocktransactionValidator(ctrl)
	txHandler.EXPECT().VerifyAndCacheTx(gomock.Any(), gomock.Any()).Return(nil)

	svc := NewTransactionService(sql.InMemory(), nil, syncer, txHandler, publisher)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := spacemeshv2alpha1.NewTransactionServiceClient(conn)

	signer, err := signing.NewEdSigner()
	addr := wallet.Address(signer.PublicKey().Bytes())
	require.NoError(t, err)
	tx := newTx(0, addr, signer)
	serializedTx, err := codec.Encode(tx)
	req.NoError(err, "error serializing tx")

	// This time, we expect an error, since isSynced is false (by default)
	// The node should not allow tx submission when not synced
	res, err := c.SubmitTransaction(ctx, &spacemeshv2alpha1.SubmitTransactionRequest{Transaction: serializedTx})
	req.Error(err)
	grpcStatus, ok := status.FromError(err)
	req.True(ok)
	req.Equal(codes.FailedPrecondition, grpcStatus.Code())
	req.Equal("Cannot submit transaction, node is not in sync yet, try again later", grpcStatus.Message())
	req.Nil(res)

	syncer.EXPECT().IsSynced(gomock.Any()).Return(true)

	// This time, we expect no error, since isSynced is now true
	_, err = c.SubmitTransaction(ctx, &spacemeshv2alpha1.SubmitTransactionRequest{Transaction: serializedTx})
	req.NoError(err)
}

func TestTransactionServiceSubmitInvalidTx(t *testing.T) {
	req := require.New(t)

	ctrl := gomock.NewController(t)
	syncer := NewMocktransactionSyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(true)
	publisher := pubsubmocks.NewMockPublisher(ctrl) // publish is not called
	txHandler := NewMocktransactionValidator(ctrl)
	txHandler.EXPECT().VerifyAndCacheTx(gomock.Any(), gomock.Any()).Return(errors.New("failed validation"))

	svc := NewTransactionService(sql.InMemory(), nil, syncer, txHandler, publisher)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := spacemeshv2alpha1.NewTransactionServiceClient(conn)

	signer, err := signing.NewEdSigner()
	addr := wallet.Address(signer.PublicKey().Bytes())
	require.NoError(t, err)
	tx := newTx(0, addr, signer)
	serializedTx, err := codec.Encode(tx)
	req.NoError(err, "error serializing tx")

	// When verifying and caching the transaction fails we expect an error
	res, err := c.SubmitTransaction(ctx, &spacemeshv2alpha1.SubmitTransactionRequest{Transaction: serializedTx})
	req.Error(err)
	grpcStatus, ok := status.FromError(err)
	req.True(ok)
	req.Equal(codes.InvalidArgument, grpcStatus.Code())
	req.Contains(grpcStatus.Message(), "Failed to verify transaction")
	req.Nil(res)
}

func TestTransactionService_SubmitNoConcurrency(t *testing.T) {
	numTxs := 20

	ctrl := gomock.NewController(t)
	syncer := NewMocktransactionSyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(true).Times(numTxs)
	publisher := pubsubmocks.NewMockPublisher(ctrl)
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(numTxs)
	txHandler := NewMocktransactionValidator(ctrl)
	txHandler.EXPECT().VerifyAndCacheTx(gomock.Any(), gomock.Any()).Return(nil).Times(numTxs)

	svc := NewTransactionService(sql.InMemory(), nil, syncer, txHandler, publisher)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := spacemeshv2alpha1.NewTransactionServiceClient(conn)

	signer, err := signing.NewEdSigner()
	addr := wallet.Address(signer.PublicKey().Bytes())
	require.NoError(t, err)
	tx := newTx(0, addr, signer)
	for range numTxs {
		res, err := c.SubmitTransaction(ctx, &spacemeshv2alpha1.SubmitTransactionRequest{
			Transaction: tx.Raw,
		})
		require.NoError(t, err)
		require.Equal(t, int32(code.Code_OK), res.Status.Code)
		require.Equal(t, tx.ID.Bytes(), res.TxId)
	}
}

func newTx(nonce uint64, recipient types.Address, signer *signing.EdSigner) *types.Transaction {
	tx := types.Transaction{TxHeader: &types.TxHeader{}}
	tx.Principal = wallet.Address(signer.PublicKey().Bytes())
	if nonce == 0 {
		tx.RawTx = types.NewRawTx(wallet.SelfSpawn(signer.PrivateKey(),
			0,
			sdk.WithGasPrice(0),
		))
	} else {
		tx.RawTx = types.NewRawTx(
			wallet.Spend(signer.PrivateKey(), recipient, 1,
				nonce,
				sdk.WithGasPrice(0),
			),
		)
		tx.MaxSpend = 1
	}
	return &tx
}
