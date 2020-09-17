package api

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"time"
)

// Service is an interface for receiving messages via gossip
type Service interface {
	RegisterGossipProtocol(string, priorityq.Priority) chan service.GossipMessage
}

// StateAPI is an API to global state
// TODO: Remove me once the old API is removed. These are not used by the new
// API implementation as this interface has been completely folded into the TxAPI.
// see https://github.com/spacemeshos/go-spacemesh/issues/2077
type StateAPI interface {
	GetBalance(address types.Address) uint64
	GetNonce(address types.Address) uint64
	Exist(address types.Address) bool
}

// NetworkAPI is an API to nodes gossip network
type NetworkAPI interface {
	Broadcast(channel string, data []byte) error
	SubscribePeerEvents() (conn, disc chan p2pcrypto.PublicKey)
}

// MiningAPI is an API for controlling Post, setting coinbase account and getting mining stats
type MiningAPI interface {
	StartPost(address types.Address, datadir string, space uint64) error
	SetCoinbaseAccount(rewardAddress types.Address)
	// MiningStats returns state of post init, coinbase reward account and data directory path for post commitment
	MiningStats() (postStatus int, remainingBytes uint64, coinbaseAccount string, postDatadir string)
	GetSmesherID() types.NodeID
	Stop()
}

// OracleAPI gets eligible layers from oracle
type OracleAPI interface {
	GetEligibleLayers() []types.LayerID
}

// GenesisTimeAPI is an API to get genesis time and current layer of the system
type GenesisTimeAPI interface {
	GetGenesisTime() time.Time
	GetCurrentLayer() types.LayerID
}

// LoggingAPI is an API to system loggers
type LoggingAPI interface {
	SetLogLevel(loggerName, severity string) error
}

// PostAPI is an API for post init module
type PostAPI interface {
	Reset() error
}

// Syncer is the API to get sync status and to start sync
type Syncer interface {
	IsSynced() bool
	Start()
}

// TxAPI is an api for getting transaction status
type TxAPI interface {
	AddressExists(types.Address) bool
	ValidateNonceAndBalance(*types.Transaction) error
	GetATXs([]types.ATXID) (map[types.ATXID]*types.ActivationTx, []types.ATXID)
	GetLayer(types.LayerID) (*types.Layer, error)
	GetRewards(types.Address) ([]types.Reward, error)
	GetTransactions([]types.TransactionID) ([]*types.Transaction, map[types.TransactionID]struct{})
	GetTransactionsByDestination(types.LayerID, types.Address) []types.TransactionID
	GetTransactionsByOrigin(types.LayerID, types.Address) []types.TransactionID
	LatestLayer() types.LayerID
	GetLayerApplied(types.TransactionID) *types.LayerID
	GetTransaction(types.TransactionID) (*types.Transaction, error)
	GetProjection(types.Address, uint64, uint64) (uint64, uint64, error)
	LatestLayerInState() types.LayerID
	ProcessedLayer() types.LayerID
	GetStateRoot() types.Hash32
	GetLayerStateRoot(types.LayerID) (types.Hash32, error)
	GetBalance(types.Address) uint64
	GetNonce(types.Address) uint64
}

// PeerCounter is an api to get amount of connected peers
type PeerCounter interface {
	PeerCount() uint64
}
