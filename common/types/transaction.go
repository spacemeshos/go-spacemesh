package types

import (
	"fmt"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"strings"
)

// TransactionID is a 32-byte sha256 sum of the transaction, used as an identifier.
type TransactionID Hash32

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
func (id TransactionID) Field() log.Field { return id.Hash32().Field("tx_id") }

// TxIdsField returns a list of loggable fields for a given list of IDs
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

// SetOrigin sets the cache of the transaction's origin address.
func (t *Transaction) SetOrigin(origin Address) {
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

	t.origin = &Address{}
	t.origin.SetBytes(pubKey)
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
	t.id = &id
	return id
}

// Hash32 returns the TransactionID as a Hash32.
func (t *Transaction) Hash32() Hash32 {
	return t.ID().Hash32()
}

// ShortString returns a the first 5 characters of the ID, for logging purposes.
func (t *Transaction) ShortString() string {
	return t.ID().ShortString()
}

// String returns a string representation of the Transaction, for logging purposes.
// It implements the fmt.Stringer interface.
func (t *Transaction) String() string {
	return fmt.Sprintf("<id: %s, origin: %s, recipient: %s, amount: %v, nonce: %v, gas_limit: %v, fee: %v>",
		t.ID().ShortString(), t.Origin().Short(), t.Recipient.Short(), t.Amount, t.AccountNonce, t.GasLimit, t.Fee)
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
}

// NewSignedTx is used in TESTS ONLY to generate signed txs
func NewSignedTx(nonce uint64, rec Address, amount, gas, fee uint64, signer *signing.EdSigner) (*Transaction, error) {
	inner := InnerTransaction{
		AccountNonce: nonce,
		Recipient:    rec,
		Amount:       amount,
		GasLimit:     gas,
		Fee:          fee,
	}

	buf, err := InterfaceToBytes(&inner)
	if err != nil {
		return nil, err
	}

	sst := &Transaction{
		InnerTransaction: inner,
		Signature:        [64]byte{},
	}

	copy(sst.Signature[:], signer.Sign(buf))
	addr := Address{}
	addr.SetBytes(signer.PublicKey().Bytes())
	sst.SetOrigin(addr)

	return sst, nil
}
