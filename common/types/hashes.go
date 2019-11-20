package types

import (
	"bytes"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/sha256-simd"
	"math/big"
	"math/rand"
	"reflect"
	"sort"
)

const (
	// HashLength is the expected length of the hash
	Hash32Length = 32
	Hash12Length = 12
)

// Hash represents the first 12 bytes of sha256, mostly used for internal caches
type Hash12 [Hash12Length]byte

// Hash represents the 32 byte Keccak256 hash of arbitrary data.
type Hash32 [Hash32Length]byte

func CalcHash12(data []byte) Hash12 {
	msghash := sha256.Sum256(data)
	var h [Hash12Length]byte
	copy(h[:], msghash[0:Hash12Length])
	return Hash12(h)
}

func CalcBlocksHash12(view []BlockID) (Hash12, error) {
	sortedView := make([]BlockID, len(view))
	copy(sortedView, view)
	sort.Slice(sortedView, func(i, j int) bool {
		return bytes.Compare(sortedView[i].ToBytes(), sortedView[j].ToBytes()) < 0
	})
	viewBytes, err := InterfaceToBytes(sortedView)
	if err != nil {
		return Hash12{}, err
	}
	return CalcHash12(viewBytes), err
}

func CalcBlocksHash32(view []BlockID) (Hash32, error) {
	sortedView := make([]BlockID, len(view))
	copy(sortedView, view)
	sort.Slice(sortedView, func(i, j int) bool {
		return bytes.Compare(sortedView[i].ToBytes(), sortedView[j].ToBytes()) < 0
	})
	viewBytes, err := InterfaceToBytes(sortedView)
	if err != nil {
		return Hash32{}, err
	}
	return CalcHash32(viewBytes), err
}

func CalcAtxsHash12(ids []AtxId) (Hash12, error) {
	sorted := make([]AtxId, len(ids))
	copy(sorted, ids)
	sort.Slice(sorted, func(i, j int) bool {
		a := sorted[i].Hash32()
		b := sorted[j].Hash32()
		return bytes.Compare(a[:], b[:]) < 0 //sorted[i] < sorted[j]
	})
	bytes, err := InterfaceToBytes(sorted)
	if err != nil {
		return Hash12{}, err
	}
	return CalcHash12(bytes), err
}

func CalcTxsHash12(ids []TransactionId) (Hash12, error) {
	sorted := make([]TransactionId, len(ids))
	copy(sorted, ids)
	sort.Slice(sorted, func(i, j int) bool {
		return bytes.Compare(sorted[i][:], sorted[j][:]) < 0 //sorted[i] < sorted[j]
	})
	bytes, err := TxIdsAsBytes(sorted)
	if err != nil {
		return Hash12{}, err
	}
	return CalcHash12(bytes), err
}

func CalcMessageHash12(msg []byte, prot string) Hash12 {
	return CalcHash12(append(msg, []byte(prot)...))
}

var (
	hashT = reflect.TypeOf(Hash32{})
)

func CalcHash32(data []byte) Hash32 {
	return sha256.Sum256(data)
}

func CalcAtxHash32(atx *ActivationTx) Hash32 {
	bytes, err := InterfaceToBytes(&atx.ActivationTxHeader)
	if err != nil {
		panic("could not Serialize atx")
	}
	return CalcHash32(bytes)
}

// BytesToHash sets b to hash.
// If b is larger than len(h), b will be cropped from the left.
func BytesToHash(b []byte) Hash32 {
	var h Hash32
	h.SetBytes(b)
	return h
}

// BigToHash sets byte representation of b to hash.
// If b is larger than len(h), b will be cropped from the left.
func BigToHash(b *big.Int) Hash32 { return BytesToHash(b.Bytes()) }

// HexToHash sets byte representation of s to hash.
// If b is larger than len(h), b will be cropped from the left.
func HexToHash32(s string) Hash32 { return BytesToHash(util.FromHex(s)) }

// Bytes gets the byte representation of the underlying hash.
func (h Hash32) Bytes() []byte { return h[:] }

// Big converts a hash to a big integer.
func (h Hash32) Big() *big.Int { return new(big.Int).SetBytes(h[:]) }

// Hex converts a hash to a hex string.
func (h Hash32) Hex() string { return util.Encode(h[:]) }

// TerminalString implements log.TerminalStringer, formatting a string for console
// output during logging.
func (h Hash32) TerminalString() string {
	return fmt.Sprintf("%x…%x", h[:3], h[29:])
}

// String implements the stringer interface and is used also by the logger when
// doing full logging into a file.
func (h Hash32) String() string {
	return h.Hex()
}

func (h Hash32) ShortString() string {
	l := len(h.Hex())
	return h.Hex()[util.Min(2, l):util.Min(7, l)]
}

// Format implements fmt.Formatter, forcing the byte slice to be formatted as is,
// without going through the stringer interface used for logging.
func (h Hash32) Format(s fmt.State, c rune) {
	fmt.Fprintf(s, "%"+string(c), h[:])
}

// UnmarshalText parses a hash in hex syntax.
func (h *Hash32) UnmarshalText(input []byte) error {
	return util.UnmarshalFixedText("Hash", input, h[:])
}

// UnmarshalJSON parses a hash in hex syntax.
func (h *Hash32) UnmarshalJSON(input []byte) error {
	return util.UnmarshalFixedJSON(hashT, input, h[:])
}

// MarshalText returns the hex representation of h.
func (h Hash32) MarshalText() ([]byte, error) {
	return util.Bytes(h[:]).MarshalText()
}

// SetBytes sets the hash to the value of b.
// If b is larger than len(h), b will be cropped from the left.
func (h *Hash32) SetBytes(b []byte) {
	if len(b) > len(h) {
		b = b[len(b)-32:]
	}

	copy(h[32-len(b):], b)
}

// Generate implements testing/quick.Generator.
func (h Hash32) Generate(rand *rand.Rand, size int) reflect.Value {
	m := rand.Intn(len(h))
	for i := len(h) - 1; i > m; i-- {
		h[i] = byte(rand.Uint32())
	}
	return reflect.ValueOf(h)
}

// Scan implements Scanner for database/sql.
func (h *Hash32) Scan(src interface{}) error {
	srcB, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("can't scan %T into Hash", src)
	}
	if len(srcB) != Hash32Length {
		return fmt.Errorf("can't scan []byte of len %d into Hash, want %d", len(srcB), Hash32Length)
	}
	copy(h[:], srcB)
	return nil
}
