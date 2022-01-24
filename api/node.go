package api

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

// Publisher interface for publishing messages.
type Publisher = pubsub.Publisher

// PostSetupAPI is an alias to PostSetupProvider.
type PostSetupAPI = activation.PostSetupProvider

// SmeshingAPI is an alias to SmeshingProvider.
type SmeshingAPI = activation.SmeshingProvider

// GenesisTimeAPI is an API to get genesis time and current layer of the system.
type GenesisTimeAPI interface {
	GetGenesisTime() time.Time
	GetCurrentLayer() types.LayerID
}

// LoggingAPI is an API to system loggers.
type LoggingAPI interface {
	SetLogLevel(loggerName, severity string) error
}

// Syncer is the API to get sync status and to start sync.
type Syncer interface {
	IsSynced(context.Context) bool
	Start(context.Context)
}

// TxAPI is an api for getting transaction status.
type TxAPI interface {
	AddressExists(types.Address) bool
	ValidateNonceAndBalance(*types.Transaction) error
	GetATXs(context.Context, []types.ATXID) (map[types.ATXID]*types.ActivationTx, []types.ATXID)
	GetLayer(types.LayerID) (*types.Layer, error)
	GetRewards(types.Address) ([]types.Reward, error)
	GetTransactions([]types.TransactionID) ([]*types.Transaction, map[types.TransactionID]struct{})
	GetMeshTransactions([]types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{})
	GetTransactionsByAddress(types.LayerID, types.LayerID, types.Address) ([]*types.MeshTransaction, error)
	GetTransactionsByDestination(types.LayerID, types.LayerID, types.Address) ([]*types.MeshTransaction, error)
	GetTransactionsByOrigin(types.LayerID, types.LayerID, types.Address) ([]*types.MeshTransaction, error)
	LatestLayer() types.LayerID
	GetLayerApplied(types.TransactionID) *types.LayerID
	GetMeshTransaction(types.TransactionID) (*types.MeshTransaction, error)
	GetProjection(types.Address, uint64, uint64) (uint64, uint64, error)
	LatestLayerInState() types.LayerID
	ProcessedLayer() types.LayerID
	GetStateRoot() types.Hash32
	GetLayerStateRoot(types.LayerID) (types.Hash32, error)
	GetBalance(types.Address) uint64
	GetNonce(types.Address) uint64
	GetAllAccounts() (*types.MultipleAccountsState, error)
	GetRewardsBySmesherID(types.NodeID) ([]types.Reward, error)
	// TODO: fix the discrepancy between SmesherID and NodeID (see https://github.com/spacemeshos/go-spacemesh/issues/2269)
}

// NOTE that mockgen doesn't use source-mode to avoid generating mocks for all interfaces in this file.
//go:generate mockgen -package=mocks -destination=./mocks/mocks.go github.com/spacemeshos/go-spacemesh/api NetworkIdentity

// NetworkIdentity interface.
type NetworkIdentity interface {
	ID() p2p.Peer
}

// PeerCounter is an api to get amount of connected peers.
type PeerCounter interface {
	PeerCount() uint64
}

// MempoolAPI is an API for reading mempool data that's useful for API services.
type MempoolAPI interface {
	Get(types.TransactionID) (*types.Transaction, error)
	GetTxsByAddress(types.Address) []*types.Transaction
	GetProjection(types.Address, uint64, uint64) (uint64, uint64)
}

// ActivationAPI is an API for activation module.
type ActivationAPI interface {
	UpdatePoETServer(context.Context, string) error
}
