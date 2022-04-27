package types

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// NanoTX represents minimal info about a transaction for the conservative cache/mempool.
type NanoTX struct {
	Tid       types.TransactionID
	Principal types.Address
	Fee       uint64
	Received  time.Time

	Amount uint64
	Nonce  uint64
	Block  types.BlockID
	Layer  types.LayerID
}

// NewNanoTX converts a NanoTX instance from a MeshTransaction.
func NewNanoTX(mtx *types.MeshTransaction) *NanoTX {
	return &NanoTX{
		Tid:       mtx.ID(),
		Principal: mtx.Origin(),
		Fee:       mtx.GetFee(),
		Amount:    mtx.Amount,
		Nonce:     mtx.AccountNonce,
		Received:  mtx.Received,
		Block:     mtx.BlockID,
		Layer:     mtx.LayerID,
	}
}

// MaxSpending returns the maximal amount a transaction can spend.
func (n *NanoTX) MaxSpending() uint64 {
	// TODO: create SVM methods to calculate these two fields
	return n.Fee + n.Amount
}

// Better returns true if this transaction takes priority than `other`.
func (n *NanoTX) Better(other *NanoTX) bool {
	return n.Fee > other.Fee || n.Fee == other.Fee && n.Received.Before(other.Received)
}

// UpdateLayerMaybe updates the layer of a transaction if it's lower than the current value.
func (n *NanoTX) UpdateLayerMaybe(lid types.LayerID, bid types.BlockID) {
	if n.Layer == (types.LayerID{}) || lid.Before(n.Layer) {
		n.Layer = lid
		n.Block = bid
	}
}
