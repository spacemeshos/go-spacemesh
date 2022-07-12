package txs

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	"github.com/spacemeshos/go-spacemesh/system"
	txtypes "github.com/spacemeshos/go-spacemesh/txs/types"
)

type txGetter interface {
	GetMeshTransaction(types.TransactionID) (*types.MeshTransaction, error)
}

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type conservativeState interface {
	HasTx(types.TransactionID) (bool, error)
	Validation(types.RawTx) system.ValidationRequest
	AddToCache(*types.Transaction) error
	AddToDB(*types.Transaction) error
}

type vmState interface {
	Validation(types.RawTx) system.ValidationRequest
	GetStateRoot() (types.Hash32, error)
	GetLayerStateRoot(types.LayerID) (types.Hash32, error)
	GetLayerApplied(types.TransactionID) (types.LayerID, error)
	GetAllAccounts() ([]*types.Account, error)
	GetBalance(types.Address) (uint64, error)
	GetNonce(types.Address) (types.Nonce, error)
	Revert(types.LayerID) (types.Hash32, error)
	Apply(vm.ApplyContext, []types.RawTx, []types.AnyReward) ([]types.TransactionID, []types.TransactionWithResult, error)
}

type conStateCache interface {
	GetMempool() map[types.Address][]*txtypes.NanoTX
}
