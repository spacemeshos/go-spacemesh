package txs

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/system"
)

//go:generate mockgen -package=txs -destination=./txs_mocks.go -source=./interface.go

type conservativeState interface {
	HasTx(types.TransactionID) (bool, error)
	Validation(types.RawTx) system.ValidationRequest
	AddToCache(context.Context, *types.Transaction, time.Time) error
	AddToDB(*types.Transaction) error
	GetMeshTransaction(types.TransactionID) (*types.MeshTransaction, error)
}

type vmState interface {
	Validation(types.RawTx) system.ValidationRequest
	GetStateRoot() (types.Hash32, error)
	GetLayerStateRoot(types.LayerID) (types.Hash32, error)
	GetLayerApplied(types.TransactionID) (types.LayerID, error)
	GetAllAccounts() ([]*types.Account, error)
	GetBalance(types.Address) (uint64, error)
	GetNonce(types.Address) (types.Nonce, error)
}

type conStateCache interface {
	GetMempool(log.Log) map[types.Address][]*NanoTX
}
