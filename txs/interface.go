package txs

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type conservativeState interface {
	AddressExists(addr types.Address) bool
	AddTxToMempool(tx *types.Transaction, checkValidity bool) error
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
