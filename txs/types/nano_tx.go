package types

import (
	"bytes"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
)

// NanoTX represents minimal info about a transaction for the conservative cache/mempool.
type NanoTX struct {
	types.TxHeader
	ID types.TransactionID

	Received time.Time

	Block types.BlockID
	Layer types.LayerID
}

// NewNanoTX converts a NanoTX instance from a MeshTransaction.
func NewNanoTX(mtx *types.MeshTransaction) *NanoTX {
	return &NanoTX{
		ID:       mtx.ID,
		TxHeader: *mtx.TxHeader,
		Received: mtx.Received,
		Block:    mtx.BlockID,
		Layer:    mtx.LayerID,
	}
}

// MaxSpending returns the maximal amount a transaction can spend.
func (n *NanoTX) MaxSpending() uint64 {
	return n.Spending()
}

func (n *NanoTX) combinedHash(blockSeed []byte) []byte {
	hash := hash.New()
	hash.Write(blockSeed)
	hash.Write(n.ID.Bytes())
	return hash.Sum(nil)
}

// Better returns true if this transaction takes priority than `other`.
// when the block seed is non-empty, this tx is being considered for a block.
// the block seed then is used to tie-break (deterministically) transactions for
// the same account/nonce.
func (n *NanoTX) Better(other *NanoTX, blockSeed []byte) bool {
	if n.Principal != other.Principal ||
		n.Nonce != other.Nonce {
		log.Panic("invalid arguments")
	}
	if n.Fee() > other.Fee() {
		return true
	}
	if n.Fee() == other.Fee() {
		if len(blockSeed) > 0 {
			return bytes.Compare(n.combinedHash(blockSeed), other.combinedHash(blockSeed)) < 0
		}
		return n.Received.Before(other.Received)
	}
	return false
}

// UpdateLayerMaybe updates the layer of a transaction if it's lower than the current value.
func (n *NanoTX) UpdateLayerMaybe(lid types.LayerID, bid types.BlockID) {
	if n.Layer == (types.LayerID{}) || lid.Before(n.Layer) {
		n.Layer = lid
		n.Block = bid
	}
}
