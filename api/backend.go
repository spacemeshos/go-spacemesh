package api

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

// SyncProgress gives progress indications when the node is synchronising with
// the Spacemesh network.
type SyncProgress struct {
	StartingBlock uint64 // Block number where sync began
	CurrentBlock  uint64 // Current block number where sync is at
	HighestBlock  uint64 // Highest alleged block number in the chain
}

// Implements the general Spacemesh API functions.

type Backend interface {
	// General Spacemesh API
	SyncProgress() SyncProgress
	SuggestGasTipCap(ctx context.Context) (uint64, error)

	// Blockchain API
	CurrentBlock() *types.Block
	BlocksByLayer(ctx context.Context, layerid types.LayerID) ([]*types.Block, error)
	BlockByHash(ctx context.Context, hash types.BlockID) (*types.Block, error)
	AccountByLayer(ctx context.Context, layerid types.LayerID) (*types.Account, error)

	// Transaction pool API
	GetPoolNonce(ctx context.Context, addr types.Address) (uint64, error)
	GetTransaction(ctx context.Context, txHash types.TransactionID) (*types.Transaction, types.TransactionID, uint64, uint64, error)
	GetPoolTransaction(txHash types.TransactionID) *types.Transaction
	GetPoolTransactions() ([]*types.Transaction, error)

	// GenesisID is Hash20
	ChainConfig() types.Hash20
}
