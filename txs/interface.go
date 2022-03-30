package txs

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type conservativeState interface {
	HasTx(types.TransactionID) bool
	AddressExists(addr types.Address) bool
	AddTxToMemPool(tx *types.Transaction, checkValidity bool) error
}

type svmState interface {
	GetStateRoot() types.Hash32
	GetLayerStateRoot(types.LayerID) (types.Hash32, error)
	GetLayerApplied(types.TransactionID) *types.LayerID
	GetAllAccounts() (*types.MultipleAccountsState, error)
	AddressExists(types.Address) bool
	GetBalance(types.Address) uint64
	GetNonce(types.Address) uint64
	Rewind(types.LayerID) (types.Hash32, error)
	ApplyLayer(types.LayerID, []*types.Transaction, map[types.Address]uint64) ([]*types.Transaction, error)
}

type conStateCache interface {
	GetMempool() (map[types.Address][]*types.NanoTX, error)
}

type txProvider interface {
	add(*types.Transaction, time.Time) error
	has(types.TransactionID) (bool, error)
	get(types.TransactionID) (*types.MeshTransaction, error)
	getBlob(types.TransactionID) ([]byte, error)
	getByAddress(types.LayerID, types.LayerID, types.Address) ([]*types.MeshTransaction, error)
	addToProposal(types.LayerID, types.ProposalID, []types.TransactionID) error
	addToBlock(types.LayerID, types.BlockID, []types.TransactionID) error
	undoApply(types.LayerID) error
	discard4Ever(types.TransactionID) error
	applyLayer(types.LayerID, types.BlockID, []types.TransactionID, []types.TransactionID) error
	setNextLayerBlock(types.TransactionID, types.LayerID) (types.LayerID, types.BlockID, error)
	getAllPending() ([]*types.MeshTransaction, error)
	getAcctPending(types.Address) ([]*types.MeshTransaction, error)
}
