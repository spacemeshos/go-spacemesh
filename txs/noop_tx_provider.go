package txs

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// a no-op txs provider for cache to build the transactions for block.
type nopTP struct{}

func (ntp *nopTP) Add(*types.Transaction, time.Time) error                 { return nil }
func (ntp *nopTP) Has(types.TransactionID) (bool, error)                   { return false, nil }
func (ntp *nopTP) Get(types.TransactionID) (*types.MeshTransaction, error) { return nil, nil }
func (ntp *nopTP) GetBlob(types.TransactionID) ([]byte, error)             { return nil, nil }
func (ntp *nopTP) GetByAddress(types.LayerID, types.LayerID, types.Address) ([]*types.MeshTransaction, error) {
	return nil, nil
}

func (ntp *nopTP) AddToProposal(types.LayerID, types.ProposalID, []types.TransactionID) error {
	return nil
}
func (ntp *nopTP) AddToBlock(types.LayerID, types.BlockID, []types.TransactionID) error { return nil }
func (ntp *nopTP) UndoLayers(types.LayerID) error                                       { return nil }
func (ntp *nopTP) ApplyLayer(types.LayerID, types.BlockID, types.Address, map[uint64]types.TransactionID) error {
	return nil
}
func (ntp *nopTP) DiscardNonceBelow(types.Address, uint64) error { return nil }
func (ntp *nopTP) SetNextLayerBlock(types.TransactionID, types.LayerID) (types.LayerID, types.BlockID, error) {
	return types.LayerID{}, types.EmptyBlockID, nil
}
func (ntp *nopTP) GetAllPending() ([]*types.MeshTransaction, error) { return nil, nil }
func (ntp *nopTP) GetAcctPendingFromNonce(types.Address, uint64) ([]*types.MeshTransaction, error) {
	return nil, nil
}
func (ntp *nopTP) LastAppliedLayer() (types.LayerID, error)        { return types.LayerID{}, nil }
func (ntp *nopTP) GetMeshHash(types.LayerID) (types.Hash32, error) { return types.EmptyLayerHash, nil }
