package types

import (
	"bytes"
	"crypto/sha512"
	"errors"
	"fmt"
	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"strings"
)

// TransactionTypesMap defines transaction types interpretation
var TransactionTypesMap = map[TransactionType]TransactionTypeRoute{
	TxSimpleCoinEdPlus: {"TxSimpleCoinEdPlus", EdPlusSigning, DecodeSimpleCoinTx},
	TxSimpleCoinEd:     {"TxSimpleCoinEd", EdSigning, DecodeSimpleCoinTx},
	TxCallAppEdPlus:    {"TxCallAppEdPlus", EdPlusSigning, DecodeCallAppTx},
	TxCallAppEd:        {"TxCallAppEd", EdSigning, DecodeCallAppTx},
	TxSpawnAppEdPlus:   {"TxSpawnAppEdPlus", EdPlusSigning, DecodeSpawnAppTx},
	TxSpawnAppEd:       {"TxSpawnAppEd", EdSigning, DecodeSpawnAppTx},
	TxOldCoinEdPlus:    {"TxOldCoinEdPlus", EdPlusSigning, DecodeOldCoinTx},
	TxOldCoinEd:        {"TxOldCoinEd", EdSigning, DecodeOldCoinTx},
	// add new routes here
}

// TransactionType is a transaction's kind and signing scheme
type TransactionType byte

const (
	// TxSimpleCoinEd is a simple coin transaction with ed signing scheme
	TxSimpleCoinEd TransactionType = 0
	// TxSimpleCoinEdPlus is a simple coin transaction with ed++ signing scheme
	TxSimpleCoinEdPlus TransactionType = 1
	// TxCallAppEd is a exec app transaction with ed signing scheme
	TxCallAppEd TransactionType = 2
	// TxCallAppEdPlus is a exec app transaction with ed++ signing scheme
	TxCallAppEdPlus TransactionType = 3
	// TxSpawnAppEd is a spawn app transaction with ed signing scheme
	TxSpawnAppEd TransactionType = 4
	// TxSpawnAppEdPlus is a spawn app transaction with ed++ signing scheme
	TxSpawnAppEdPlus TransactionType = 5

	// for support code transition to new transactions abstraction

	// TxOldCoinEd is a old coin transaction with ed signing scheme
	TxOldCoinEd TransactionType = 6
	// TxOldCoinEdPlus is a old coin transaction with ed++ signing scheme
	TxOldCoinEdPlus TransactionType = 7
)

// TransactionTypeRoute defines how to interpret transaction type
type TransactionTypeRoute struct {
	Name    string
	Signing SigningScheme
	Decode  func([]byte, TransactionType) (IncompleteTransaction, error)
}

// Good checks that transaction type is Good
func (tt TransactionType) Good() bool {
	_, ok := TransactionTypesMap[tt]
	return ok
}

// String returns string representation for TransactionType
func (tt TransactionType) String() string {
	if v, ok := TransactionTypesMap[tt]; ok {
		return v.Name
	}
	return "UnknownTransactionType"
}

func (tt TransactionType) Scheme() SigningScheme {
	if v, ok := TransactionTypesMap[tt]; ok {
		return v.Signing
	}
	panic(fmt.Errorf("unknown transaction type"))
}

// Decode decodes transaction bytes into the transaction object
func (tt TransactionType) Decode(data []byte) (IncompleteTransaction, error) {
	if v, ok := TransactionTypesMap[tt]; ok {
		return v.Decode(data, tt)
	}
	return nil, fmt.Errorf("unknown transaction type")
}

// EdPlusTransactionFactory allowing to create transactions with Ed++ signing scheme
type EdPlusTransactionFactory interface {
	NewEdPlus() IncompleteTransaction
}

// EdTransactionFactory allowing to create transactions with Ed signing scheme
type EdTransactionFactory interface {
	NewEd() IncompleteTransaction
}

// GeneralTransaction is the general immutable transactions interface
type GeneralTransaction interface {
	fmt.Stringer // String()string

	AuthenticationMessage() (TransactionAuthenticationMessage, error)
	Type() TransactionType
	Digest() ([]byte, error)

	// extract internal transaction structure
	Extract(interface{}) bool

	// common attributes
	// they use Get prefix to not mess with struct attributes
	//    and do not pass function address to log/printf instead value

	GetRecipient() Address
	GetAmount() uint64
	GetNonce() uint64
	GetGasLimit() uint64
	GetGasPrice() uint64
	GetFee(gas uint64) uint64
}

// IncompleteTransaction is the interface of an immutable incomplete transaction
type IncompleteTransaction interface {
	GeneralTransaction
	Complete(key TxPublicKey, signature TxSignature, id TransactionID) Transaction
}

// SignTransaction signs incomplete transaction and returns completed transaction object
func SignTransaction(itx IncompleteTransaction, signer *signing.EdSigner) (tx Transaction, err error) {
	txm, err := itx.AuthenticationMessage()
	if err != nil {
		return
	}
	stx, err := txm.Sign(signer)
	if err != nil {
		return
	}
	return stx.Decode()
}

// Transaction is the interface of an immutable complete transaction
type Transaction interface {
	GeneralTransaction
	Origin() Address
	ID() TransactionID
	Hash32() Hash32
	ShortString() string
	PubKey() TxPublicKey
	Signature() TxSignature
	Encode() (SignedTransaction, error)
	Prune() Transaction
	Pruned() bool
}

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

// TransactionAuthenticationMessage is an incomplete transaction binary representation
type TransactionAuthenticationMessage struct {
	TxType          TransactionType
	Digest          [sha512.Size]byte // contains type, network id and transaction immutable data
	TransactionData []byte
}

// Type returns transaction type
func (txm TransactionAuthenticationMessage) Type() TransactionType {
	return txm.TxType
}

// Type returns signing scheme
func (txm TransactionAuthenticationMessage) Scheme() SigningScheme {
	return txm.TxType.Scheme()
}

// Sign signs transaction binary data
func (txm TransactionAuthenticationMessage) Sign(signer *signing.EdSigner) (_ SignedTransaction, err error) {
	signature := txm.Scheme().Sign(signer, txm.Digest[:])
	return txm.Encode(TxPublicKeyFromBytes(signer.PublicKey().Bytes()), signature)
}

// Encode encodes transaction into the independent form
func (txm TransactionAuthenticationMessage) Encode(pubKey TxPublicKey, signature TxSignature) (_ SignedTransaction, err error) {
	stl := SignedTransactionLayout{TxType: [1]byte{byte(txm.TxType)}, Data: txm.TransactionData, Signature: signature}
	extractablePubKey := txm.Scheme().ExtractablePubKey()
	if !extractablePubKey {
		stl.PubKey = pubKey.Bytes()
	}
	bf := bytes.Buffer{}
	if _, err = xdr.Marshal(&bf, &stl); err != nil {
		return
	}
	bs := bf.Bytes()
	if extractablePubKey {
		bs = bs[:len(bs)-4] // remove 4 bytes of zero length pubkey
	}
	return bs, nil
}

// Verify verifies transaction bytes
func (txm TransactionAuthenticationMessage) Verify(pubKey TxPublicKey, sig TxSignature) bool {
	return txm.Scheme().Verify(txm.Digest[:], pubKey, sig)
}

// SignedTransactionLayout represents fields layout of a signed transaction
type SignedTransactionLayout struct {
	TxType    [1]byte
	Signature [TxSignatureLength]byte
	Data      []byte
	PubKey    [] /*TODO:???*/ byte
}

// Type returns transaction type
func (stl SignedTransactionLayout) Type() TransactionType {
	return TransactionType(stl.TxType[0])
}

// SignedTransaction is the binary transaction independent form
type SignedTransaction []byte

// ID returns transaction identifier
func (stx SignedTransaction) ID() TransactionID {
	return TransactionID(CalcHash32(stx[:]))
}

var errBadSignatureError = errors.New("failed to verify: bad signature")
var errBadTransactionEncodingError = errors.New("failed to decode: bad transaction")
var errBadTransactionTypeError = errors.New("failed to decode: bad transaction type")

// Decode decodes binary transaction into transaction object
func (stx SignedTransaction) Decode() (tx Transaction, err error) {
	stl := SignedTransactionLayout{}
	bs := append(stx[:], 0, 0, 0, 0)

	n, err := xdr.Unmarshal(bytes.NewReader(bs), &stl)
	if err != nil {
		return
	}

	if !stl.Type().Good() {
		return tx, errBadTransactionTypeError
	}

	signScheme := stl.Type().Scheme()

	extractablePubKey := signScheme.ExtractablePubKey()
	if extractablePubKey && n != 4+len(stx) || !extractablePubKey && n != len(stx) {
		// to protect against digest compilation attack with ED++
		return tx, errBadTransactionEncodingError
	}

	itx, err := stl.Type().Decode(stl.Data)
	if err != nil {
		return
	}

	digest, err := itx.Digest()
	if err != nil {
		return
	}

	var pubKey TxPublicKey
	if pk, ok, err := signScheme.Extract(digest, stl.Signature); ok {
		if err != nil {
			return tx, fmt.Errorf("failed to verify transaction: %v", err.Error())
		}
		pubKey = pk
	} else {
		if len(stl.PubKey) != TxPublicKeyLength {
			return tx, errBadSignatureError
		}
		pubKey = TxPublicKeyFromBytes(stl.PubKey)
		if !signScheme.Verify(digest, pubKey, stl.Signature) {
			return tx, errBadSignatureError
		}
	}

	return itx.Complete(pubKey, stl.Signature, stx.ID()), nil
}
