package types

import (
	"bytes"
	"errors"
	"fmt"
	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/log"
	"strings"
)

// EnableTransactionPruning enables support for transaction pruning and changes how prunable transactions are signed
var EnableTransactionPruning = false

const (
	// TxEdScheme is the classic Ed25519 scheme
	TxEdScheme byte = 0
	// TxEdPlusScheme is the extended Ed25519++ scheme
	TxEdPlusScheme byte = 1
)

const (
	// TxSimpleCoin is a simple coin transaction
	TxSimpleCoin byte = 0
	// TxCallApp is a exec app transaction
	TxCallApp byte = 2
	// TxSpawnApp is a spawn app transaction
	TxSpawnApp byte = 4

	// TxOldCoin is a old coin transaction
	//   for support code transition to new transactions abstraction
	TxOldCoin byte = 6
)

// TransactionTypesMap defines transaction types interpretation
var TransactionTypesMap = map[byte]TransactionType{
	simpleCoinEdPlusType.Value: simpleCoinEdPlusType,
	simpleCoinEdType.Value:     simpleCoinEdType,
	callAppEdPlusType.Value:    callAppEdPlusType,
	callAppEdType.Value:        callAppEdType,
	spawnAppEdPlusType.Value:   spawnAppEdPlusType,
	spawnAppEdType.Value:       spawnAppEdType,
	oldCoinEdPlusType.Value:    oldCoinEdPlusType,
	oldCoinEdType.Value:        oldCoinEdType,
	// add new transaction types here
}

// TransactionType is a type interface for transactions manipulation
type TransactionType struct{ *TransactionTypeObject }

// TransactionTypeObject defines how to interpret transaction type
type TransactionTypeObject struct {
	Value   byte
	Name    string
	Signing SigningScheme
	Decode  func([]byte, TransactionType) (IncompleteTransaction, error)
}

// New creates new transaction type
func (tto TransactionTypeObject) New() TransactionType {
	return TransactionType{&tto}
}

// String returns name of transaction type
func (tt TransactionType) String() string {
	return tt.Name
}

// Kind returns kind of transaction like TxSimpleCoin, TxCallApp, ...
func (tt TransactionType) Kind() byte {
	return tt.Value & ^byte(1)
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
	String() string

	Message() (TransactionMessage, error)
	Type() TransactionType
	Digest() (TransactionDigest, error)

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
	Complete(key PublicKey, signature Signature, id TransactionID) Transaction
}

// SignTransaction signs incomplete transaction and returns completed transaction object
func SignTransaction(itx IncompleteTransaction, signer Signer) (tx Transaction, err error) {
	txm, err := itx.Message()
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
	PubKey() PublicKey
	Signature() Signature
	Encode() (SignedTransaction, error)

	// implementation must be enabled bye setting EnableTransactionPruning to true
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

// TransactionMessage is an incomplete transaction binary representation
type TransactionMessage struct {
	Type   TransactionType
	Digest TransactionDigest // hashed type, network id and transaction immutable data
	Data   []byte
}

// Scheme returns signing scheme
func (txm TransactionMessage) Scheme() SigningScheme {
	return txm.Type.Signing
}

// Sign signs transaction binary data
func (txm TransactionMessage) Sign(signer Signer) (_ SignedTransaction, err error) {
	signature := txm.Scheme().Sign(signer, txm.Digest[:])
	return txm.Encode(signer.PublicKey(), signature)
}

// Encode encodes transaction into the independent form
func (txm TransactionMessage) Encode(pubKey PublicKey, signature Signature) (_ SignedTransaction, err error) {
	stl := SignedTransactionLayout{TxType: [1]byte{txm.Type.Value}, Data: txm.Data, Signature: signature}
	extractable := txm.Scheme().Extractable
	if !extractable {
		stl.PubKey = pubKey.Bytes()
	}
	bf := bytes.Buffer{}
	if _, err = xdr.Marshal(&bf, &stl); err != nil {
		return
	}
	bs := bf.Bytes()
	if extractable {
		bs = bs[:len(bs)-4] // remove 4 bytes of zero length pubkey
	}
	return bs, nil
}

// Verify verifies transaction bytes
func (txm TransactionMessage) Verify(pubKey PublicKey, signature Signature) bool {
	return txm.Scheme().Verify(txm.Digest, pubKey, signature)
}

// SignedTransactionLayout represents fields layout of a signed transaction for XDR encoder
type SignedTransactionLayout struct {
	TxType    [1]byte
	Signature [SignatureLength]byte
	Data      []byte
	PubKey    [] /*TODO:???*/ byte
}

// Type returns transaction type
func (stl SignedTransactionLayout) Type() byte {
	return stl.TxType[0]
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

	txType, good := TransactionTypesMap[stl.Type()]
	if !good {
		return tx, errBadTransactionTypeError
	}

	extractable := txType.Signing.Extractable
	if extractable && n != 4+len(stx) || !extractable && n != len(stx) {
		// to protect against digest compilation attack with ED++
		return tx, errBadTransactionEncodingError
	}

	itx, err := txType.Decode(stl.Data, txType)
	if err != nil {
		return
	}

	digest, err := itx.Digest()
	if err != nil {
		return
	}

	var pubKey PublicKey
	if extractable {
		pubKey, _, err = txType.Signing.ExtractPubKey(digest, stl.Signature)
		if err != nil {
			return tx, fmt.Errorf("failed to verify transaction: %v", err.Error())
		}
	} else {
		if len(stl.PubKey) != txType.Signing.PubKeyLength {
			return tx, errBadSignatureError
		}
		pubKey = txType.Signing.NewPubKey(stl.PubKey)
		if !txType.Signing.Verify(digest, pubKey, stl.Signature) {
			return tx, errBadSignatureError
		}
	}

	return itx.Complete(pubKey, stl.Signature, stx.ID()), nil
}
