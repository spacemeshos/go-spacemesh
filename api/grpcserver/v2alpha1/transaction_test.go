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
	pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	"github.com/spacemeshos/go-spacemesh/txs"
	"github.com/spacemeshos/go-spacemesh/vm"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/sdk"
	sdkmultisig "github.com/spacemeshos/go-spacemesh/vm/sdk/multisig"
	"github.com/spacemeshos/go-spacemesh/vm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/vm/templates/multisig"
	walletTemplate "github.com/spacemeshos/go-spacemesh/vm/templates/wallet"
)

func TestTransactionService_List(t *testing.T) {
	types.SetLayersPerEpoch(5)
	db := statesql.InMemoryTest(t)
	ctx := context.Background()

	gen := fixture.NewTransactionResultGenerator().WithAddresses(2)
	txsList := make([]types.TransactionWithResult, 100)
	require.NoError(t, db.WithTxImmediate(ctx, func(dtx sql.Transaction) error {
		for i := range txsList {
			tx := gen.Next(t)

			require.NoError(t, transactions.Add(dtx, &tx.Transaction, time.Time{}))
			require.NoError(t, transactions.AddResult(dtx, tx.ID, &tx.TransactionResult))
			txsList[i] = *tx
		}
		return nil
	}))

	svc := NewTransactionService(db, nil, nil, nil, nil)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(t, cfg)
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

	t.Run("address", func(t *testing.T) {
		address := txsList[0].Principal.String()
		var expectedTxs []types.TransactionWithResult
		for _, tx := range txsList {
			found := false
			if tx.Transaction.Principal.String() == address {
				found = true
			}

			for _, addr := range tx.TransactionResult.Addresses {
				if addr.String() == address {
					found = true
					break
				}
			}
			if found {
				expectedTxs = append(expectedTxs, tx)
			}
		}

		list, err := client.List(ctx, &spacemeshv2alpha1.TransactionRequest{
			Address: &address,
			Limit:   100,
		})
		require.NoError(t, err)
		require.Len(t, list.Transactions, len(expectedTxs))
	})

	t.Run("address/startlayer/endlayer", func(t *testing.T) {
		address := txsList[0].Principal.String()
		layer := txsList[0].Layer.Uint32()
		var expectedTxs []types.TransactionWithResult
		for _, tx := range txsList {
			found := false
			if tx.Transaction.Principal.String() == address &&
				tx.Layer.Uint32() >= layer && tx.Layer.Uint32() <= layer {
				found = true
			}

			for _, addr := range tx.TransactionResult.Addresses {
				if addr.String() == address &&
					tx.Layer.Uint32() >= layer && tx.Layer.Uint32() <= layer {
					found = true
					break
				}
			}
			if found {
				expectedTxs = append(expectedTxs, tx)
			}
		}
		list, err := client.List(ctx, &spacemeshv2alpha1.TransactionRequest{
			Address:    &address,
			StartLayer: &layer,
			EndLayer:   &layer,
			Limit:      100,
		})
		require.NoError(t, err)
		require.Len(t, list.Transactions, len(expectedTxs))
	})

	t.Run("address/txid", func(t *testing.T) {
		address := txsList[0].Principal.String()
		list, err := client.List(ctx, &spacemeshv2alpha1.TransactionRequest{
			Address: &address,
			Txid:    [][]byte{txsList[0].ID[:]},
			Limit:   100,
		})
		require.NoError(t, err)
		require.Len(t, list.Transactions, 1)
		require.Equal(t, txsList[0].TxHeader.Principal.String(), list.Transactions[0].Tx.Principal)
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
	db := statesql.InMemoryTest(t)
	vminst := vm.New(db)
	ctx := context.Background()

	svc := NewTransactionService(db, txs.NewConservativeState(vminst, db), nil, nil, nil)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	keys := make([]signing.PrivateKey, 4)
	accounts := make([]types.Account, len(keys)+1)
	rng := rand.New(rand.NewSource(10101))
	for i := range keys {
		pub, priv, err := ed25519.GenerateKey(rng)
		require.NoError(t, err)
		keys[i] = priv
		address := wallet.Address(pub)
		accounts[i] = types.Account{Address: address, Balance: 1e12}
	}
	accounts[len(keys)] = types.Account{
		Address:         walletTemplate.TemplateAddress,
		State:           walletTemplate.PROGRAM,
		TemplateAddress: &walletTemplate.TemplateAddress,
	}
	require.NoError(t, vminst.ApplyGenesis(accounts))
	tx, err := wallet.Spawn(keys[0], 0)
	require.NoError(t, err)
	_, _, err = vminst.Apply(
		types.GetEffectiveGenesis().Add(1),
		[]types.Transaction{{RawTx: types.NewRawTx(tx)}},
		nil,
	)
	require.NoError(t, err)

	conn := dialGrpc(t, cfg)
	client := spacemeshv2alpha1.NewTransactionServiceClient(conn)

	t.Run("valid tx", func(t *testing.T) {
		tx, err := wallet.Spend(keys[0], accounts[3].Address, 100, 0)
		require.NoError(t, err)
		resp, err := client.EstimateGas(ctx, &spacemeshv2alpha1.EstimateGasRequest{
			Transaction: tx,
		})
		require.NoError(t, err)
		require.NotZero(t, resp.RecommendedMaxGas)
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
		tx, err := wallet.Spend(keys[2], accounts[3].Address, 100, 0)
		require.NoError(t, err)
		_, err = client.EstimateGas(ctx, &spacemeshv2alpha1.EstimateGasRequest{
			Transaction: tx,
		})
		s, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, s.Code())
		assert.Contains(t, s.Message(), "not spawned")
	})
}

func TestTransactionService_ParseTransaction(t *testing.T) {
	types.SetLayersPerEpoch(5)
	db := statesql.InMemoryTest(t)
	vminst := vm.New(db)
	ctx := context.Background()

	svc := NewTransactionService(db, txs.NewConservativeState(vminst, db), nil, nil, nil)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	keys := make([]signing.PrivateKey, 4)
	accounts := make([]types.Account, len(keys)+1)
	rng := rand.New(rand.NewSource(10101))
	for i := range keys {
		pub, priv, err := ed25519.GenerateKey(rng)
		require.NoError(t, err)
		keys[i] = priv
		addr := wallet.Address(pub)
		accounts[i] = types.Account{Address: addr, Balance: 1e12}
	}
	accounts[len(keys)] = types.Account{
		Address:         walletTemplate.TemplateAddress,
		State:           walletTemplate.PROGRAM,
		TemplateAddress: &walletTemplate.TemplateAddress,
	}
	require.NoError(t, vminst.ApplyGenesis(accounts))
	tx, err := wallet.Spawn(keys[0], 0)
	require.NoError(t, err)
	_, _, err = vminst.Apply(
		types.GetEffectiveGenesis().Add(1),
		[]types.Transaction{{RawTx: types.NewRawTx(tx)}},
		nil,
	)
	require.NoError(t, err)

	mangled, err := wallet.Spend(keys[0], accounts[3].Address, 100, 0)
	require.NoError(t, err)
	mangled[len(mangled)-1] -= 1

	conn := dialGrpc(t, cfg)
	client := spacemeshv2alpha1.NewTransactionServiceClient(conn)

	t.Run("valid tx", func(t *testing.T) {
		tx, err := wallet.Spend(keys[0], accounts[3].Address, 100, 0)
		require.NoError(t, err)
		resp, err := client.ParseTransaction(ctx, &spacemeshv2alpha1.ParseTransactionRequest{
			Transaction: tx,
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp)
	})
	t.Run("valid tx with verify set to true", func(t *testing.T) {
		tx, err := wallet.Spend(keys[0], accounts[3].Address, 100, 0)
		require.NoError(t, err)
		resp, err := client.ParseTransaction(ctx, &spacemeshv2alpha1.ParseTransactionRequest{
			Transaction: tx,
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
		tx, err := wallet.Spend(keys[2], accounts[3].Address, 100, 0)
		require.NoError(t, err)
		_, err = client.ParseTransaction(ctx, &spacemeshv2alpha1.ParseTransactionRequest{
			Transaction: tx,
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
		assert.Contains(t, s.Message(), "tx failed verification")
	})
	t.Run("verify transaction contents for spend tx", func(t *testing.T) {
		addr := accounts[3].Address
		amount := uint64(100)
		tx, err := wallet.Spend(keys[0], addr, amount, 0)
		require.NoError(t, err)
		resp, err := client.ParseTransaction(ctx, &spacemeshv2alpha1.ParseTransactionRequest{
			Transaction: tx,
			Verify:      true,
		})
		require.NoError(t, err)

		require.Equal(t, amount, resp.Tx.Contents.GetSend().Amount)
		require.Equal(t, addr.String(), resp.Tx.Contents.GetSend().Destination)
	})

	t.Run("transaction contents for spawn tx", func(t *testing.T) {
		var publicKey core.PublicKey
		copy(publicKey[:], signing.Public(keys[1]))
		tx, err := wallet.Spawn(keys[1], 0)
		require.NoError(t, err)
		resp, err := client.ParseTransaction(ctx, &spacemeshv2alpha1.ParseTransactionRequest{
			Transaction: tx,
			Verify:      true,
		})

		require.NoError(t, err)
		require.Equal(t, publicKey.String(), resp.Tx.Contents.GetSingleSigSpawn().Pubkey)
	})
	t.Run("transaction contents for deploy tx", func(t *testing.T) {
		code := []byte("contract template code")
		tx, err := wallet.Deploy(keys[0], 0, code)
		require.NoError(t, err)
		resp, err := client.ParseTransaction(ctx, &spacemeshv2alpha1.ParseTransactionRequest{
			Transaction: tx,
			Verify:      true,
		})
		require.NoError(t, err)
		require.Equal(t, spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_DEPLOY, resp.Tx.GetType())
		deployContents := resp.Tx.Contents.GetDeploy()
		require.NotNil(t, deployContents)
		require.Equal(t, core.TemplateAddress(code).String(), deployContents.Template)
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

	svc := NewTransactionService(statesql.InMemoryTest(t), nil, syncer, txHandler, publisher)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(t, cfg)
	c := spacemeshv2alpha1.NewTransactionServiceClient(conn)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	addr := wallet.Address(signer.PublicKey().Bytes())
	tx := newTx(t, 0, addr, signer)
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

	svc := NewTransactionService(statesql.InMemoryTest(t), nil, syncer, txHandler, publisher)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(t, cfg)
	c := spacemeshv2alpha1.NewTransactionServiceClient(conn)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	addr := wallet.Address(signer.PublicKey().Bytes())
	tx := newTx(t, 0, addr, signer)
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

	svc := NewTransactionService(statesql.InMemoryTest(t), nil, syncer, txHandler, publisher)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialGrpc(t, cfg)
	c := spacemeshv2alpha1.NewTransactionServiceClient(conn)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	addr := wallet.Address(signer.PublicKey().Bytes())
	tx := newTx(t, 0, addr, signer)
	for range numTxs {
		res, err := c.SubmitTransaction(ctx, &spacemeshv2alpha1.SubmitTransactionRequest{
			Transaction: tx.Raw,
		})
		require.NoError(t, err)
		require.Equal(t, int32(code.Code_OK), res.Status.Code)
		require.Equal(t, tx.ID.Bytes(), res.TxId)
	}
}

func newTx(t *testing.T, nonce uint64, recipient types.Address, signer *signing.EdSigner) *types.Transaction {
	tx := types.Transaction{TxHeader: &types.TxHeader{}}
	principal := wallet.Address(signer.PublicKey().Bytes())
	tx.Principal = principal
	if nonce == 0 {
		tx2, err := wallet.Spawn(signer.PrivateKey(), 0, sdk.WithGasPrice(0))
		require.NoError(t, err)
		tx.RawTx = types.NewRawTx(tx2)
	} else {
		tx2, err := wallet.Spend(signer.PrivateKey(), recipient, 1, nonce, sdk.WithGasPrice(0))
		require.NoError(t, err)
		tx.RawTx = types.NewRawTx(tx2)
		tx.MaxSpend = 1
	}
	return &tx
}

func TestToTxContents(t *testing.T) {
	t.Parallel()

	t.Run("singlesig spawn", func(t *testing.T) {
		t.Parallel()

		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		tx := newTx(t, 0, types.Address{}, signer)

		contents, txType, err := toTxContents(context.Background(), tx.Raw, &types.TxHeader{TemplateAddress: walletTemplate.TemplateAddress})
		require.NoError(t, err)
		require.NotNil(t, contents.GetSingleSigSpawn())
		require.Nil(t, contents.GetSend())
		require.Equal(t, spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_SINGLE_SIG_SPAWN, txType)
	})

	t.Run("singlesig send", func(t *testing.T) {
		t.Parallel()

		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		tx := newTx(t, 1, types.Address{}, signer)

		contents, txType, err := toTxContents(context.Background(), tx.Raw, &types.TxHeader{TemplateAddress: walletTemplate.TemplateAddress})
		require.NoError(t, err)
		require.NotNil(t, contents.GetSend())
		require.Nil(t, contents.GetSingleSigSpawn())
		require.Equal(t, spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_SINGLE_SIG_SEND, txType)
	})

	t.Run("multisig spawn", func(t *testing.T) {
		t.Parallel()

		var (
			pubs    []core.PublicKey
			pubStrs []string
			pks     []ed25519.PrivateKey
		)

		for i := 0; i < 3; i++ {
			pub, pk, err := ed25519.GenerateKey(nil)
			require.NoError(t, err)
			pks = append(pks, pk)
			p := core.PublicKey(pub)
			pubs = append(pubs, p)
			pubStrs = append(pubStrs, p.String())
		}

		tx, err := sdkmultisig.Spawn(multisig.TemplateAddress, 2, pubs, 0)
		require.NoError(t, err)
		agg := sdkmultisig.NewSignatureAggregator(tx)
		for i := range 2 {
			sig := core.SignRawTx(tx, types.Hash20{}, pks[i])
			agg.Add(uint8(i), core.Signature(sig))
		}
		rawTx := agg.Raw()
		contents, txType, err := toTxContents(context.Background(), rawTx, &types.TxHeader{TemplateAddress: multisig.TemplateAddress})
		require.NoError(t, err)
		require.NotNil(t, contents.GetMultiSigSpawn())
		require.Equal(t, &spacemeshv2alpha1.ContentsMultiSigSpawn{
			Required: 2,
			Pubkey:   pubStrs,
		}, contents.GetMultiSigSpawn())
		require.Nil(t, contents.GetSend())
		require.Nil(t, contents.GetSingleSigSpawn())
		require.Nil(t, contents.GetVestingSpawn())
		require.Nil(t, contents.GetVaultSpawn())
		require.Nil(t, contents.GetDrainVault())
		require.Equal(t, spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_MULTI_SIG_SPAWN, txType)
	})

	t.Run("multisig send", func(t *testing.T) {
		t.Parallel()

		var (
			pks  []ed25519.PrivateKey
			to   = types.RandomAddress(t)
			from = types.RandomAddress(t)
		)
		for i := 0; i < 3; i++ {
			_, pk, err := ed25519.GenerateKey(nil)
			require.NoError(t, err)
			pks = append(pks, pk)
		}

		tx, err := sdkmultisig.Spend(from, to, 100, 1)
		require.NoError(t, err)
		agg := sdkmultisig.NewSignatureAggregator(tx)
		for i := range 2 {
			sig := core.SignRawTx(tx, types.Hash20{}, pks[i])
			agg.Add(uint8(i), core.Signature(sig))
		}
		rawTx := agg.Raw()

		contents, txType, err := toTxContents(context.Background(), rawTx, &types.TxHeader{TemplateAddress: multisig.TemplateAddress})
		require.NoError(t, err)
		require.NotNil(t, contents.GetSend())
		require.Equal(t, &spacemeshv2alpha1.ContentsSend{
			Destination: to.String(),
			Amount:      100,
		}, contents.GetSend())
		require.Nil(t, contents.GetMultiSigSpawn())
		require.Nil(t, contents.GetSingleSigSpawn())
		require.Nil(t, contents.GetVestingSpawn())
		require.Nil(t, contents.GetVaultSpawn())
		require.Nil(t, contents.GetDrainVault())
		require.Equal(t, spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_MULTI_SIG_SEND, txType)
	})

	t.Run("vault spawn", func(t *testing.T) {
		t.Skip("vault spawn is not supported yet")
		// t.Parallel()

		// var pubs []ed25519.PublicKey
		// pks := make([]ed25519.PrivateKey, 0, 3)
		// for i := 0; i < 3; i++ {
		// 	pub, pk, err := ed25519.GenerateKey(nil)
		// 	require.NoError(t, err)
		// 	pubs = append(pubs, pub)
		// 	pks = append(pks, pk)
		// }

		// owner, err := wallet.Address(*signing.NewPublicKey(pubs[0]))
		// require.NoError(t, err)
		// vaultArgs := &vault.SpawnArguments{
		// 	Owner:               owner,
		// 	InitialUnlockAmount: uint64(1000),
		// 	TotalAmount:         uint64(1001),
		// 	VestingStart:        105120,
		// 	VestingEnd:          4 * 105120,
		// }
		// vaultAddr := types.Address{}
		// // vaultAddr := core.ComputePrincipalFromBlob(vault.TemplateAddress, vaultArgs)

		// var agg *multisig2.Aggregator
		// for i := 0; i < len(pks); i++ {
		// 	part := multisig2.Spawn(uint8(i), pks[i], vaultAddr, vault.TemplateAddress, vaultArgs, types.Nonce(0))
		// 	if agg == nil {
		// 		agg = part
		// 	} else {
		// 		agg.Add(*part.Part(uint8(i)))
		// 	}
		// }
		// rawTx := agg.Raw()

		// contents, txType, err := toTxContents(rawTx)
		// require.NoError(t, err)
		// require.NotNil(t, contents.GetVaultSpawn())
		// require.Nil(t, contents.GetMultiSigSpawn())
		// require.Nil(t, contents.GetSingleSigSpawn())
		// require.Nil(t, contents.GetVestingSpawn())
		// require.Nil(t, contents.GetSend())
		// require.Nil(t, contents.GetDrainVault())
		// require.Equal(t, vaultArgs.Owner.String(), contents.GetVaultSpawn().Owner)
		// require.Equal(t, vaultArgs.InitialUnlockAmount, contents.GetVaultSpawn().InitialUnlockAmount)
		// require.Equal(t, vaultArgs.TotalAmount, contents.GetVaultSpawn().TotalAmount)
		// require.Equal(t, vaultArgs.VestingStart.Uint32(), contents.GetVaultSpawn().VestingStart)
		// require.Equal(t, vaultArgs.VestingEnd.Uint32(), contents.GetVaultSpawn().VestingEnd)
		// require.Equal(t, spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_VAULT_SPAWN, txType)
	})

	t.Run("drain vault", func(t *testing.T) {
		t.Skip("drain vault is not supported yet")
		// t.Parallel()

		// var pubs [][]byte
		// pks := make([]ed25519.PrivateKey, 0, 3)
		// for i := 0; i < 3; i++ {
		// 	pub, pk, err := ed25519.GenerateKey(nil)
		// 	require.NoError(t, err)
		// 	pubs = append(pubs, pub)
		// 	pks = append(pks, pk)
		// }

		// principal := multisig2.Address(multisig.TemplateAddress, 3, pubs...)
		// to, err := wallet.Address(*signing.NewPublicKey(pubs[1]))
		// require.NoError(t, err)
		// vaultAddr, err := wallet.Address(*signing.NewPublicKey(pubs[2]))
		// require.NoError(t, err)

		// agg := vesting.DrainVault(
		// 	0,
		// 	pks[0],
		// 	principal,
		// 	vaultAddr,
		// 	to,
		// 	100,
		// 	types.Nonce(1))
		// for i := 1; i < len(pks); i++ {
		// 	part := vesting.DrainVault(uint8(i), pks[i], principal, vaultAddr, to, 100, types.Nonce(1))
		// 	agg.Add(*part.Part(uint8(i)))
		// }
		// rawTx := agg.Raw()

		// contents, txType, err := toTxContents(rawTx)
		// require.NoError(t, err)
		// require.NotNil(t, contents.GetDrainVault())
		// require.Nil(t, contents.GetMultiSigSpawn())
		// require.Nil(t, contents.GetSingleSigSpawn())
		// require.Nil(t, contents.GetVestingSpawn())
		// require.Nil(t, contents.GetSend())
		// require.Nil(t, contents.GetVaultSpawn())
		// require.Equal(t, vaultAddr.String(), contents.GetDrainVault().Vault)
		// require.Equal(t, spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_DRAIN_VAULT, txType)
	})

	t.Run("multisig vesting spawn", func(t *testing.T) {
		t.Skip("multisig vesting spawn is not supported yet")
		// t.Parallel()

		// var pubs []ed25519.PublicKey
		// pks := make([]ed25519.PrivateKey, 0, 3)
		// for i := 0; i < 3; i++ {
		// 	pub, pk, err := ed25519.GenerateKey(nil)
		// 	require.NoError(t, err)
		// 	pubs = append(pubs, pub)
		// 	pks = append(pks, pk)
		// }

		// var agg *multisig2.Aggregator
		// for i := 0; i < len(pks); i++ {
		// 	part := multisig2.SelfSpawn(uint8(i), pks[i], vesting2.TemplateAddress, 1, pubs, types.Nonce(1))
		// 	if agg == nil {
		// 		agg = part
		// 	} else {
		// 		agg.Add(*part.Part(uint8(i)))
		// 	}
		// }
		// rawTx := agg.Raw()

		// contents, txType, err := toTxContents(rawTx)
		// require.NoError(t, err)
		// require.NotNil(t, contents.GetVestingSpawn())
		// require.Nil(t, contents.GetSend())
		// require.Nil(t, contents.GetSingleSigSpawn())
		// require.Nil(t, contents.GetMultiSigSpawn())
		// require.Nil(t, contents.GetVaultSpawn())
		// require.Nil(t, contents.GetDrainVault())
		// require.Equal(t, spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_VESTING_SPAWN, txType)
	})
}

func TestEvictedTransaction(t *testing.T) {
	req := require.New(t)

	db := statesql.InMemoryTest(t)
	ctrl := gomock.NewController(t)
	syncer := NewMocktransactionSyncer(ctrl)
	publisher := pubsubmocks.NewMockPublisher(ctrl)
	txHandler := NewMocktransactionValidator(ctrl)
	conState := NewMocktransactionConState(ctrl)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	tx := newTx(t, 0, types.Address{}, signer)
	require.NoError(t, transactions.Add(db, &types.Transaction{
		RawTx:    tx.RawTx,
		TxHeader: nil,
	}, time.Now()))

	svc := NewTransactionService(db, conState, syncer, txHandler, publisher)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(t, cfg)
	client := spacemeshv2alpha1.NewTransactionServiceClient(conn)

	conState.EXPECT().HasEvicted(gomock.Any()).Return(true, nil)
	list, err := client.List(context.Background(), &spacemeshv2alpha1.TransactionRequest{Limit: 1, IncludeState: true})
	req.NoError(err)
	req.Len(list.Transactions, 1)
	req.Equal(tx.ID.Bytes(), list.Transactions[0].Tx.Id)
	req.Equal(list.Transactions[0].TxState.String(),
		spacemeshv2alpha1.TransactionState_TRANSACTION_STATE_INEFFECTUAL.String())
}
