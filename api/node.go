package api

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"math/big"
)

type StateAPI interface {
	GetBalance(address common.Address) *big.Int

	GetNonce(address common.Address) uint64

	Exist(address common.Address) bool
}

type NetworkAPI interface {
	Broadcast(channel string, data []byte)
}
