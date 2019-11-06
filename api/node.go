package api

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"time"
)

type Service interface {
	RegisterGossipProtocol(string) chan service.GossipMessage
}

type StateAPI interface {
	GetBalance(address types.Address) uint64

	GetNonce(address types.Address) uint64

	Exist(address types.Address) bool
}

type NetworkAPI interface {
	Broadcast(channel string, data []byte) error
}

type MiningAPI interface {
	StartPost(address types.Address, datadir string, space uint64) error
	SetCoinbaseAccount(rewardAddress types.Address)
	// MiningStats returns state of post init, coinbase reward account and data directory path for post commitment
	MiningStats() (int, string, string)
}

type OracleAPI interface {
	GetEligibleLayers() []types.LayerID
}

type GenesisTimeAPI interface {
	GetGenesisTime() time.Time
}

type LoggingAPI interface {
	SetLogLevel(loggerName, severity string) error
}
