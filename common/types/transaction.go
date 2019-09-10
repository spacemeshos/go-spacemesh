package types

import (
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/sha256-simd"
)

type TransactionId Hash32

func (id TransactionId) Hash32() Hash32 {
	return Hash32(id)
}

func (id TransactionId) ShortString() string {
	return id.Hash32().ShortString()
}

func (id TransactionId) String() string {
	return id.Hash32().String()
}

func (id TransactionId) Bytes() []byte {
	return id[:]
}

var EmptyTransactionId = TransactionId{}

type Transaction struct {
	InnerTransaction
	Signature [64]byte
	origin    *Address
	id        *TransactionId
}

func (t *Transaction) Origin() Address {
	if t.origin != nil {
		return *t.origin
	}

	txBytes, err := InterfaceToBytes(&t.InnerTransaction)
	if err != nil {
		panic("failed to marshal transaction: " + err.Error())
	}
	pubKey, err := ed25519.ExtractPublicKey(txBytes, t.Signature[:])
	if err != nil {
		panic("failed to extract transaction pubkey: " + err.Error())
	}

	t.origin = &Address{}
	t.origin.SetBytes(pubKey)
	return *t.origin
}

func (t *Transaction) Id() TransactionId {
	if t.id != nil {
		return *t.id
	}

	txBytes, err := InterfaceToBytes(t)
	if err != nil {
		panic("failed to marshal transaction: " + err.Error())
	}
	id := TransactionId(sha256.Sum256(txBytes))
	t.id = &id
	return id
}

type InnerTransaction struct {
	AccountNonce uint64
	Recipient    Address
	GasLimit     uint64
	GasPrice     uint64
	Amount       uint64
}

// TEST ONLY
func NewTxWithOrigin(nonce uint64, orig, rec Address, amount, gasLimit, gasPrice uint64) *Transaction {
	inner := InnerTransaction{
		AccountNonce: nonce,
		Recipient:    rec,
		Amount:       amount,
		GasLimit:     gasLimit,
		GasPrice:     gasPrice,
	}
	return &Transaction{
		InnerTransaction: inner,
		origin:           &orig,
	}
}

// TEST ONLY
func NewSignedTx(nonce uint64, rec Address, amount, gas, price uint64, signer *signing.EdSigner) (*Transaction, error) {
	inner := InnerTransaction{
		AccountNonce: nonce,
		Recipient:    rec,
		Amount:       amount,
		GasLimit:     gas,
		GasPrice:     price,
	}

	buf, err := InterfaceToBytes(&inner)
	if err != nil {
		log.Error("failed to marshal tx")
		return nil, err
	}

	sst := &Transaction{
		InnerTransaction: inner,
		Signature:        [64]byte{},
		origin:           &Address{},
	}

	copy(sst.Signature[:], signer.Sign(buf))
	sst.origin.SetBytes(signer.PublicKey().Bytes())

	return sst, nil
}
