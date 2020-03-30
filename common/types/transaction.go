package types

import (
	"fmt"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/log"
)

type TransactionID Hash32

func (id TransactionID) Hash32() Hash32 {
	return Hash32(id)
}

func (id TransactionID) ShortString() string {
	return id.Hash32().ShortString()
}

func (id TransactionID) String() string {
	return id.Hash32().String()
}

func (id TransactionID) Bytes() []byte {
	return id[:]
}

func (id TransactionID) Field() log.Field { return id.Hash32().Field("tx_id") }

var EmptyTransactionID = TransactionID{}

type Transaction struct {
	InnerTransaction
	Signature [64]byte
	origin    *Address
	id        *TransactionID
}

func (t *Transaction) Origin() Address {
	if t.origin == nil {
		panic("origin not set")
	}
	return *t.origin
}

func (t *Transaction) SetOrigin(origin Address) {
	t.origin = &origin
}

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

func (t *Transaction) Hash32() Hash32 {
	return t.ID().Hash32()
}

func (t *Transaction) ShortString() string {
	return t.ID().ShortString()
}

func (t *Transaction) String() string {
	return fmt.Sprintf("<id: %s, origin: %s, recipient: %s, amount: %v, nonce: %v, gas_limit: %v, fee: %v>",
		t.ID().ShortString(), t.Origin().Short(), t.Recipient.Short(), t.Amount, t.AccountNonce, t.GasLimit, t.Fee)
}

type InnerTransaction struct {
	AccountNonce uint64
	Recipient    Address
	GasLimit     uint64
	Fee          uint64
	Amount       uint64
}

type Reward struct {
	Layer               LayerID
	TotalReward         uint64
	LayerRewardEstimate uint64
}
