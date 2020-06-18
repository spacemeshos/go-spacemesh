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
