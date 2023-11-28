package api

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/system"
)

// SyncProgress gives progress indications when the node is synchronising with
// the Spacemesh network.
type SyncProgress struct {
	StartingBlock uint64 // Block number where sync began
	CurrentBlock  uint64 // Current block number where sync is at
	HighestBlock  uint64 // Highest alleged block number in the chain
}

// ConservativeState is an API for reading state and transaction/mempool data.
type ConservativeState interface {
	GetStateRoot() (types.Hash32, error)
	GetLayerStateRoot(types.LayerID) (types.Hash32, error)
	GetAllAccounts() ([]*types.Account, error)
	GetBalance(types.Address) (uint64, error)
	GetNonce(types.Address) (types.Nonce, error)
	GetProjection(types.Address) (uint64, uint64)
	GetMeshTransaction(types.TransactionID) (*types.MeshTransaction, error)
	GetMeshTransactions([]types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{})
	GetTransactionsByAddress(types.LayerID, types.LayerID, types.Address) ([]*types.MeshTransaction, error)
	Validation(raw types.RawTx) system.ValidationRequest
}

type BlockLayerOrHash struct {
	BlockLayer       *BlockLayer    `json:"blockLayer,omitempty"`
	BlockHash        *types.BlockID `json:"blockHash,omitempty"`
	RequireCanonical bool           `json:"requireCanonical,omitempty"`
}

type BlockLayer int64

const (
	LatestBlockLayer = BlockLayer(-1)
)

func BlockLayerOrHashWithNumber(blockLayer BlockLayer) BlockLayerOrHash {
	return BlockLayerOrHash{
		BlockLayer:       &blockLayer,
		BlockHash:        nil,
		RequireCanonical: false,
	}
}

func BlockLayerOrHashWithHash(hash types.BlockID, canonical bool) BlockLayerOrHash {
	return BlockLayerOrHash{
		BlockLayer:       nil,
		BlockHash:        &hash,
		RequireCanonical: canonical,
	}
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
	BlocksByLayerOrHash(ctx context.Context, blockLayerOrHash BlockLayerOrHash) ([]*types.Block, error)
	AccountByLayer(ctx context.Context, layerid types.LayerID) (*types.Account, error)

	// Transaction pool API
	// note: there is currently no way to get pool txs by account. is this needed?
	GetProjection(types.Address) (uint64, uint64)
	GetTransaction(ctx context.Context, txHash types.TransactionID) (*types.Transaction, types.BlockID, uint64, uint64, error)
	GetPoolTransaction(txHash types.TransactionID) *types.Transaction
	GetPoolTransactions() ([]*types.Transaction, error)

	// GenesisID is Hash20
	ChainConfig() types.Hash20
}
