package types

import (
	"bytes"
	"sort"
	"strings"
	"time"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate scalegen -types Transaction,Reward,RawTx

// TransactionID is a 32-byte blake3 sum of the transaction, used as an identifier.
type TransactionID Hash32

const (
	// TransactionIDSize in bytes.
	TransactionIDSize = Hash32Length
)

// Hash32 returns the TransactionID as a Hash32.
func (id TransactionID) Hash32() Hash32 {
	return Hash32(id)
}

// ShortString returns a the first 10 characters of the ID, for logging purposes.
func (id TransactionID) ShortString() string {
	return id.Hash32().ShortString()
}

// String returns a hexadecimal representation of the TransactionID with "0x" prepended, for logging purposes.
// It implements the fmt.Stringer interface.
func (id TransactionID) String() string {
	return id.Hash32().String()
}

// Bytes returns the TransactionID as a byte slice.
func (id TransactionID) Bytes() []byte {
	return id[:]
}

// Field returns a log field. Implements the LoggableField interface.
func (id TransactionID) Field() log.Field { return log.FieldNamed("tx_id", id.Hash32()) }

// Compare returns true if other (the given TransactionID) is less than this TransactionID, by lexicographic comparison.
func (id TransactionID) Compare(other TransactionID) bool {
	return bytes.Compare(id.Bytes(), other.Bytes()) < 0
}

// EncodeScale implements scale codec interface.
func (id *TransactionID) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(e, id[:])
}

// DecodeScale implements scale codec interface.
func (id *TransactionID) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, id[:])
}

// TxIdsField returns a list of loggable fields for a given list of IDs.
func TxIdsField(ids []TransactionID) log.Field {
	strs := []string{}
	for _, a := range ids {
		strs = append(strs, a.ShortString())
	}
	return log.String("tx_ids", strings.Join(strs, ", "))
}

// Transaction is an alias to RawTx.
type Transaction struct {
	RawTx
	*TxHeader
}

// GetRaw returns raw bytes of the transaction with id.
func (t Transaction) GetRaw() RawTx {
	return t.RawTx
}

// Verified returns true if header is set.
func (t Transaction) Verified() bool {
	return t.TxHeader != nil
}

// Hash32 returns the TransactionID as a Hash32.
func (t *Transaction) Hash32() Hash32 {
	return t.ID.Hash32()
}

// ShortString returns the first 5 characters of the ID, for logging purposes.
func (t *Transaction) ShortString() string {
	return t.ID.ShortString()
}

// ToTransactionIDs returns a slice of TransactionID corresponding to the given transactions.
func ToTransactionIDs(txs []*Transaction) []TransactionID {
	ids := make([]TransactionID, 0, len(txs))
	for _, tx := range txs {
		ids = append(ids, tx.ID)
	}
	return ids
}

// SortTransactionIDs sorts a list of TransactionID in their lexicographic order, in-place.
func SortTransactionIDs(ids []TransactionID) []TransactionID {
	sort.Slice(ids, func(i, j int) bool { return ids[i].Compare(ids[j]) })
	return ids
}

// TransactionIDsToHashes turns a list of TransactionID into their Hash32 representation.
func TransactionIDsToHashes(ids []TransactionID) []Hash32 {
	hashes := make([]Hash32, 0, len(ids))
	for _, id := range ids {
		hashes = append(hashes, id.Hash32())
	}
	return hashes
}

// TXState describes the state of a transaction.
type TXState uint32

const (
	// PENDING represents the state when a transaction is syntactically valid, but its nonce and
	// the principal's ability to cover gas have not been verified yet.
	PENDING TXState = iota
	// MEMPOOL represents the state when a transaction is in mempool.
	MEMPOOL
	// APPLIED represents the state when a transaction is applied to the state.
	APPLIED
)

// MeshTransaction is stored in the mesh and included in the block.
type MeshTransaction struct {
	Transaction
	LayerID  LayerID
	BlockID  BlockID
	State    TXState
	Received time.Time
}

// Reward is a virtual reward transaction, which the node keeps track of for the gRPC api.
type Reward struct {
	Layer       LayerID
	TotalReward uint64
	LayerReward uint64
	Coinbase    Address
}

// NewRawTx computes id from raw bytes and returns the object.
func NewRawTx(raw []byte) RawTx {
	return RawTx{
		ID:  hash.Sum(raw),
		Raw: raw,
	}
}

// RawTx stores an identity and a pointer to raw bytes.
type RawTx struct {
	ID  TransactionID
	Raw []byte `scale:"max=4096"` // transactions should always be less than 4kb
}

// AddressNonce is an (address, nonce) named tuple.
type AddressNonce struct {
	Address Address
	Nonce   Nonce
}
