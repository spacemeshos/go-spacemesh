package txs

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/address"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	"github.com/spacemeshos/go-spacemesh/system"
	txtypes "github.com/spacemeshos/go-spacemesh/txs/types"
)

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
	GetBalance(address.Address) (uint64, error)
	GetNonce(address.Address) (types.Nonce, error)
	Revert(types.LayerID) (types.Hash32, error)
	Apply(vm.ApplyContext, []types.RawTx, []types.AnyReward) ([]types.TransactionID, []types.TransactionWithResult, error)
}

type conStateCache interface {
	GetMempool() map[address.Address][]*txtypes.NanoTX
}

type txProvider interface {
	Add(*types.Transaction, time.Time) error
	AddHeader(types.TransactionID, *types.TxHeader) error
	Has(types.TransactionID) (bool, error)
	Get(types.TransactionID) (*types.MeshTransaction, error)
	GetByAddress(types.LayerID, types.LayerID, address.Address) ([]*types.MeshTransaction, error)
	AddToProposal(types.LayerID, types.ProposalID, []types.TransactionID) error
	AddToBlock(types.LayerID, types.BlockID, []types.TransactionID) error
	UndoLayers(types.LayerID) error
	ApplyLayer(map[uint64]types.TransactionWithResult) error
	DiscardNonceBelow(address.Address, uint64) error
	SetNextLayerBlock(types.TransactionID, types.LayerID) (types.LayerID, types.BlockID, error)
	GetAllPending() ([]*types.MeshTransaction, error)
	GetAcctPendingFromNonce(address.Address, uint64) ([]*types.MeshTransaction, error)
	LastAppliedLayer() (types.LayerID, error)
	GetMeshHash(types.LayerID) (types.Hash32, error)
}
