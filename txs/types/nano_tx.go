package types

import (
	"bytes"
	"crypto/sha256"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// NanoTX represents minimal info about a transaction for the conservative cache/mempool.
type NanoTX struct {
	Tid       types.TransactionID
	Principal types.Address
	Fee       uint64 // TODO replace with gas price
	MaxGas    uint64
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
		MaxGas:    mtx.MaxGas(),
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

func (n *NanoTX) combinedHash(blockSeed []byte) []byte {
	hash := sha256.New()
	hash.Write(blockSeed)
	hash.Write(n.Tid.Bytes())
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
	if n.Fee > other.Fee {
		return true
	}
	if n.Fee == other.Fee {
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
