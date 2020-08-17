package api

import (
	"github.com/spacemeshos/go-spacemesh/activation"
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

type PostAPI = activation.PostProvider

//type PostAPI interface {
//	PostStatus() (*activation.PostStatus, error)
//	PostComputeProviders() []initialization.ComputeProvider
//	CreatePostData(options *activation.PostOptions) (chan struct{}, error)
//	StopPostDataCreationSession(deleteFiles bool) error
//	PostDataCreationProgressStream() <-chan *activation.PostStatus
//	InitCompleted() (chan struct{}, bool)
//	GenerateProof(challenge []byte) (*types.PostProof, error)
//	Cfg() config.Config
//	SetLogger(logger log.Log)
//}

type SmeshingAPI interface {
	Smeshing() bool
	StartSmeshing(coinbase types.Address) error
	StopSmeshing() error
	SmesherID() types.NodeID
	Coinbase() types.Address
	SetCoinbase(coinbase types.Address)
	MinGas() uint64
	SetMinGas(value uint64)
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
