package types

import (
	"bytes"
	"fmt"
	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/sha256-simd"
)

var CallAppEdPlusType = TransactionTypeObject{
	TxCallAppEdPlus, "TxCallAppEdPlus", EdPlusSigningScheme, DecodeCallAppTx,
}.New()

var CallAppEdType = TransactionTypeObject{
	TxCallAppEd, "TxCallAppEd", EdSigningScheme, DecodeCallAppTx,
}.New()

var SpawnAppEdPlusType = TransactionTypeObject{
	TxSpawnAppEdPlus, "TxSpawnAppEdPlus", EdPlusSigningScheme, DecodeSpawnAppTx,
}.New()

var SpawnAppEdType = TransactionTypeObject{
	TxSpawnAppEd, "TxSpawnAppEd", EdSigningScheme, DecodeSpawnAppTx,
}.New()

const (
	originalTransaction byte = 0
	prunedTransaction   byte = 0xff
)

var prunedDataHasher = sha256.New

const prunedDataHashSize int = sha256.Size

// CallAppTx implements "Call App Transaction"
type CallAppTx struct {
	TTL        uint32  // TTL TODO: update
	Nonce      byte    // Nonce TODO: update
	AppAddress Address // AppAddress Recipient App Address to Call
	Amount     uint64  // Amount of the transaction
	GasLimit   uint64  // GasLimit for the transaction
	GasPrice   uint64  // GasPrice for the transaction
	CallData   []byte  // CallData an additional data
}

type xrdCallAppTx struct {
	TTL             uint32  // TTL TODO: update
	NonceAndPrune   [2]byte // can be used as Nonce [1]byte for client encoders, because of 4 byte pudding
	Amount          uint64  // Amount of the transaction
	GasLimit        uint64  // GasLimit for the transaction
	GasPrice        uint64  // GasPrice for the transaction
	AddrAndCallData []byte  // AddrAndCallData is concatenated Address and CallData bytes
}

// NewEdPlus creates a new incomplete transaction with Ed++ signing scheme
func (h CallAppTx) NewEdPlus() IncompleteTransaction {
	tx := &incompCallAppTx{CallAppTxHeader{h, 0}, IncompleteCommonTx{txType: CallAppEdPlusType}}
	tx.self = tx
	return tx
}

// NewEd creates a new incomplete transaction with Ed signing scheme
func (h CallAppTx) NewEd() IncompleteTransaction {
	tx := &incompCallAppTx{CallAppTxHeader{h, 0}, IncompleteCommonTx{txType: CallAppEdType}}
	tx.self = tx
	return tx
}

// incompCallAppTx implements IncompleteTransaction for a "Call App Transaction"
type incompCallAppTx struct {
	CallAppTxHeader
	IncompleteCommonTx
}

// String implements fmt.Stringer interface
func (tx incompCallAppTx) String() string {
	return fmt.Sprintf(
		"<incomplete transaction, type: %v, app: %s, ttl: %d, amount: %v, nonce: %v, gas_limit: %v, gas_price: %v>",
		tx.txType,
		tx.AppAddress.Short(), tx.TTL, tx.Amount, tx.Nonce,
		tx.GasLimit, tx.GasPrice)
}

// Extract implements IncompleteTransaction.Extract to extract internal transaction structure
func (tx incompCallAppTx) Extract(out interface{}) bool {
	return tx.extract(out, tx.txType)
}

// CallAppTxHeader implements txSelf and Get* methods from IncompleteTransaction
type CallAppTxHeader struct {
	CallAppTx
	pruned byte
}

// XdrBytes implements txSelf.xdrBytes
func (h CallAppTxHeader) xdrBytes() ([]byte, error) {
	bf := bytes.Buffer{}
	adr := h.AppAddress[:]
	if h.pruned != originalTransaction {
		adr = []byte{}
	}
	d := xrdCallAppTx{
		h.TTL,
		[2]byte{h.Nonce, h.pruned},
		h.Amount,
		h.GasLimit,
		h.GasPrice,
		append(adr, h.CallData...),
	}
	if _, err := xdr.Marshal(&bf, &d); err != nil {
		return nil, err
	}
	return bf.Bytes(), nil
}

// XdrFill implements txSelf.xdrFill
func (h *CallAppTxHeader) xdrFill(bs []byte) (int, error) {
	d := xrdCallAppTx{}
	n, err := xdr.Unmarshal(bytes.NewReader(bs), &d)
	h.pruned = d.NonceAndPrune[1]
	h.CallAppTx = CallAppTx{
		TTL:      d.TTL,
		Nonce:    d.NonceAndPrune[0],
		Amount:   d.Amount,
		GasLimit: d.GasLimit,
		GasPrice: d.GasPrice,
	}
	if h.pruned == originalTransaction {
		h.AppAddress = BytesToAddress(d.AddrAndCallData[:AddressLength])
		h.CallData = d.AddrAndCallData[AddressLength:]
	} else {
		h.AppAddress = Address{}
		h.CallData = d.AddrAndCallData
	}
	return n, err
}

func (h CallAppTxHeader) immutableBytes() ([]byte, error) {
	bf := bytes.Buffer{}
	d := xrdCallAppTx{
		h.TTL,
		[2]byte{h.Nonce, 0},
		h.Amount,
		h.GasLimit,
		h.GasPrice,
		h.immutableCallData(),
	}
	if _, err := xdr.Marshal(&bf, &d); err != nil {
		return nil, err
	}
	return bf.Bytes(), nil
}

func (h CallAppTxHeader) immutableCallData() []byte {
	if h.pruned == originalTransaction {
		w := prunedDataHasher()
		_, _ = w.Write(h.AppAddress[:AddressLength])
		_, _ = w.Write(h.CallData)
		return w.Sum(nil)
	}
	return h.CallData
}

func (h CallAppTxHeader) complete() *CommonTx {
	tx2 := &callAppTx{CallAppTxHeader: h}
	tx2.CommonTx.self = tx2
	return &tx2.CommonTx
}

func (h CallAppTxHeader) extract(out interface{}, tt TransactionType) bool {
	if p, ok := out.(*CallAppTx); ok && (tt.Value == TxCallAppEd || tt.Value == TxCallAppEdPlus) {
		*p = h.CallAppTx
		return true
	}
	if p, ok := out.(*SpawnAppTx); ok && (tt.Value == TxSpawnAppEd || tt.Value == TxSpawnAppEdPlus) {
		*p = SpawnAppTx(h.CallAppTx)
		return true
	}
	return false
}

// GetRecipient returns recipient address
func (h CallAppTxHeader) GetRecipient() Address {
	return h.AppAddress
}

// GetAmount returns transaction amount
func (h CallAppTxHeader) GetAmount() uint64 {
	return h.Amount
}

// GetNonce returns transaction nonce
func (h CallAppTxHeader) GetNonce() uint64 {
	// TODO: nonce processing
	return uint64(h.Nonce)
}

// GetGasLimit returns transaction gas limit
func (h CallAppTxHeader) GetGasLimit() uint64 {
	return h.GasLimit
}

// GetGasPrice returns gas price
func (h CallAppTxHeader) GetGasPrice() uint64 {
	return h.GasPrice
}

// GetFee calculate transaction fee regarding gas spent
func (h CallAppTxHeader) GetFee(gas uint64) uint64 {
	return h.GasPrice * gas
}

// SpawnAppTx implements "Spawn App Transaction"
type SpawnAppTx CallAppTx

// NewEdPlus creates a new incomplete transaction with Ed++ signing scheme
func (h SpawnAppTx) NewEdPlus() IncompleteTransaction {
	tx := &incompCallAppTx{CallAppTxHeader{CallAppTx(h), 0}, IncompleteCommonTx{txType: SpawnAppEdPlusType}}
	tx.self = tx
	return tx
}

// NewEd creates a new incomplete transaction with Ed signing scheme
func (h SpawnAppTx) NewEd() IncompleteTransaction {
	tx := &incompCallAppTx{CallAppTxHeader{CallAppTx(h), 0}, IncompleteCommonTx{txType: SpawnAppEdType}}
	tx.self = tx
	return tx
}

// callAppTx implements TransactionInterface for "Call App Transaction" and "Spawn App Transaction"
type callAppTx struct {
	CallAppTxHeader
	CommonTx
}

// DecodeCallAppTx decodes transaction bytes into "Call App IncompleteTransaction" object
func DecodeCallAppTx(data []byte, txtp TransactionType) (r IncompleteTransaction, err error) {
	tx := &incompCallAppTx{}
	tx.self = tx
	return tx, tx.decode(data, txtp)
}

// DecodeSpawnAppTx decodes transaction bytes into "Spawn App IncompleteTransaction" object
func DecodeSpawnAppTx(data []byte, txtp TransactionType) (r IncompleteTransaction, err error) {
	tx := &incompCallAppTx{}
	tx.self = tx
	return tx, tx.decode(data, txtp)
}

// String implements fmt.Stringer interface
func (tx callAppTx) String() string {
	return fmt.Sprintf(
		"<id: %s, type: %v, origin: %s, app: %s, ttl: %d, amount: %v, nonce: %v, gas_limit: %v, gas_price: %v>",
		tx.ID().ShortString(), tx.txType, tx.Origin().Short(),
		tx.AppAddress.Short(), tx.TTL, tx.Amount, tx.Nonce,
		tx.GasLimit, tx.GasPrice)
}

// Extract implements IncompleteTransaction.Extract to extract internal transaction structure
func (tx callAppTx) Extract(out interface{}) bool {
	return tx.extract(out, tx.txType)
}

// Prune tries to reduce transaction size if it's possible
func (tx *callAppTx) Prune() Transaction {
	if tx.pruned != originalTransaction {
		return tx
	}
	if len(tx.CallData)+AddressLength < prunedDataHashSize {
		return tx
	}
	tx2 := &callAppTx{}
	*tx2 = *tx
	tx2.self = tx2
	tx2.pruned = prunedTransaction
	tx2.CallData = tx.immutableCallData()
	return tx2
}

// Prune returns true if transaction is pruned
func (tx callAppTx) Pruned() bool {
	return tx.pruned != originalTransaction
}
