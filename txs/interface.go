package txs

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	txtypes "github.com/spacemeshos/go-spacemesh/txs/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type conservativeState interface {
	HasTx(types.TransactionID) (bool, error)
	AddressExists(types.Address) bool
	AddToCache(*types.Transaction, bool) error
}

type svmState interface {
	GetStateRoot() types.Hash32
	GetLayerStateRoot(types.LayerID) (types.Hash32, error)
	GetLayerApplied(types.TransactionID) *types.LayerID
	GetAllAccounts() (*types.MultipleAccountsState, error)
	AddressExists(types.Address) bool
	GetBalance(types.Address) uint64
	GetNonce(types.Address) uint64
	Revert(types.LayerID) (types.Hash32, error)
	ApplyLayer(types.LayerID, []*types.Transaction, []types.AnyReward) ([]*types.Transaction, error)
}

type conStateCache interface {
	GetMempool() (map[types.Address][]*txtypes.NanoTX, error)
}

type txProvider interface {
	Add(*types.Transaction, time.Time) error
	Has(types.TransactionID) (bool, error)
	Get(types.TransactionID) (*types.MeshTransaction, error)
	GetBlob(types.TransactionID) ([]byte, error)
	GetByAddress(types.LayerID, types.LayerID, types.Address) ([]*types.MeshTransaction, error)
	AddToProposal(types.LayerID, types.ProposalID, []types.TransactionID) error
	AddToBlock(types.LayerID, types.BlockID, []types.TransactionID) error
	UndoLayers(types.LayerID) error
	ApplyLayer(types.LayerID, types.BlockID, types.Address, map[uint64]types.TransactionID) error
	DiscardNonceBelow(types.Address, uint64) error
	SetNextLayerBlock(types.TransactionID, types.LayerID) (types.LayerID, types.BlockID, error)
	GetAllPending() ([]*types.MeshTransaction, error)
	GetAcctPendingFromNonce(types.Address, uint64) ([]*types.MeshTransaction, error)
	LastAppliedLayer() (types.LayerID, error)
}
