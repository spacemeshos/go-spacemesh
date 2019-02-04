package api

import (
	"github.com/spacemeshos/go-spacemesh/address"
	"math/big"
)

type StateAPI interface {
	GetBalance(address address.Address) *big.Int

	GetNonce(address address.Address) uint64

	Exist(address address.Address) bool
}

type NetworkAPI interface {
	Broadcast(channel string, data []byte) error
}
