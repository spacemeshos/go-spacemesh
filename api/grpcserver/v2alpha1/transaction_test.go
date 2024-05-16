package v2alpha1

import (
	"context"
	"math/rand"
	"testing"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/txs"
)

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
