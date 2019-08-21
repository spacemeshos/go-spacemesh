package api

import (
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/types"
)

type Service interface {
	RegisterGossipProtocol(string) chan service.GossipMessage
}

type StateAPI interface {
	GetBalance(address address.Address) uint64

	GetNonce(address address.Address) uint64

	Exist(address address.Address) bool
}

type NetworkAPI interface {
	Broadcast(channel string, data []byte) error
}

type MiningAPI interface {
	StartPost(address address.Address, datadir string, space uint64) error
	SetCoinbaseAccount(rewardAddress address.Address)
	// MiningStats returns state of post init, coinbase reward account and data directory path for post commitment
	MiningStats() (int, string, string)
}

type OracleAPI interface {
	GetEligibleLayers() []types.LayerID
}
