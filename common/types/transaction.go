package types

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spacemeshos/ed25519"

	"github.com/spacemeshos/go-spacemesh/log"
)

// TransactionID is a 32-byte sha256 sum of the transaction, used as an identifier.
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

// TxIdsField returns a list of loggable fields for a given list of IDs.
func TxIdsField(ids []TransactionID) log.Field {
	strs := []string{}
	for _, a := range ids {
		strs = append(strs, a.ShortString())
	}
	return log.String("tx_ids", strings.Join(strs, ", "))
}

// EmptyTransactionID is a canonical empty TransactionID.
var EmptyTransactionID = TransactionID{}

// Transaction contains all transaction fields, including the signature and cached origin address and transaction ID.
type Transaction struct {
	InnerTransaction
	Signature [64]byte
	origin    *Address
	id        *TransactionID
}

// Origin returns the transaction's origin address: the public key extracted from the transaction signature.
func (t *Transaction) Origin() Address {
	if t.origin == nil {
		panic("origin not set")
	}

	return *t.origin
}

// SetID updates transaction ID.
func (t *Transaction) SetID(id TransactionID) {
	t.id = &id
}

// SetOrigin sets the cache of the transaction's origin address.
func (t *Transaction) SetOrigin(origin Address) {
	if t.origin != nil && *t.origin == origin {
		// Avoid data races caused by writing if origin is the same.
		return
	}

	t.origin = &origin
}

// CalcAndSetOrigin extracts the public key from the transaction's signature and caches it as the transaction's origin
// address.
func (t *Transaction) CalcAndSetOrigin() error {
	txBytes, err := InterfaceToBytes(&t.InnerTransaction)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction: %v", err)
	}
	pubKey, err := ed25519.ExtractPublicKey(txBytes, t.Signature[:])
	if err != nil {
		return fmt.Errorf("failed to extract transaction pubkey: %v", err)
	}

	t.SetOrigin(GenerateAddress(pubKey))

	return nil
}

// ID returns the transaction's ID. If it's not cached, it's calculated, cached and returned.
func (t *Transaction) ID() TransactionID {
	if t.id != nil {
		return *t.id
	}

	txBytes, err := InterfaceToBytes(t)
	if err != nil {
		panic("failed to marshal transaction: " + err.Error())
	}

	id := TransactionID(CalcHash32(txBytes))
	t.SetID(id)

	return id
}

// GetFee returns the fee of the transaction.
func (t *Transaction) GetFee() uint64 {
	return t.Fee
}

// GetRecipient returns the transaction recipient.
func (t *Transaction) GetRecipient() Address {
	return t.Recipient
}

// Hash32 returns the TransactionID as a Hash32.
func (t *Transaction) Hash32() Hash32 {
	return t.ID().Hash32()
}

// ShortString returns the first 5 characters of the ID, for logging purposes.
func (t *Transaction) ShortString() string {
	return t.ID().ShortString()
}

// String returns a string representation of the Transaction, for logging purposes.
// It implements the fmt.Stringer interface.
func (t *Transaction) String() string {
	return fmt.Sprintf("<id: %s, origin: %s, recipient: %s, amount: %v, nonce: %v, gas_limit: %v, fee: %v>",
		t.ID().ShortString(), t.Origin().Short(), t.GetRecipient().Short(), t.Amount, t.AccountNonce, t.GasLimit, t.GetFee())
}

// ToTransactionIDs returns a slice of TransactionID corresponding to the given transactions.
func ToTransactionIDs(txs []*Transaction) []TransactionID {
	ids := make([]TransactionID, 0, len(txs))
	for _, tx := range txs {
		ids = append(ids, tx.ID())
	}
	return ids
}

// SortTransactionIDs sorts a list of TransactionID in their lexicographic order, in-place.
func SortTransactionIDs(ids []TransactionID) []TransactionID {
	sort.Slice(ids, func(i, j int) bool { return ids[i].Compare(ids[j]) })
	return ids
}

// TXState describes the state of a transaction.
type TXState int

const (
	// PENDING represents the state when a transaction is in a proposal or a not-applied block.
	PENDING TXState = iota
	// APPLIED represents the state when a transaction is applied to the state.
	APPLIED
	// DELETED represents the state when a transaction is re-inserted into mempool
	// NOTE(dshulyak) transaction is marked as deleted, instead of deleting it
	// to avoid problems with data availability until we handle it properly.
	DELETED

	// only PENDING/APPLIED/DELETED are represented in the DB currently.
	// TODO: change to the states described in https://github.com/spacemeshos/go-spacemesh/issues/3094

	// MEMPOOL represents the state when a transaction is in mempool.
	MEMPOOL
)

// MeshTransaction is stored in the mesh and included in the block.
type MeshTransaction struct {
	Transaction
	LayerID  LayerID
	BlockID  BlockID
	State    TXState
	Received time.Time
}

// InnerTransaction includes all of a transaction's fields, except the signature (origin and id aren't stored).
type InnerTransaction struct {
	AccountNonce uint64
	Recipient    Address
	GasLimit     uint64
	Fee          uint64
	Amount       uint64
}

// Reward is a virtual reward transaction, which the node keeps track of for the gRPC api.
type Reward struct {
	Layer               LayerID
	TotalReward         uint64
	LayerRewardEstimate uint64
	SmesherID           NodeID
	Coinbase            Address
}
