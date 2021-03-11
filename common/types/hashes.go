package types

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/sha256-simd"
	"math/big"
	"math/rand"
	"reflect"
)

const (
	// Hash32Length is 32, the expected length of the hash
	Hash32Length = 32
	hash20Length = 20
	hash12Length = 12
)

// Hash12 represents the first 12 bytes of sha256, mostly used for internal caches
type Hash12 [hash12Length]byte

// Hash32 represents the 32-byte sha256 hash of arbitrary data.
type Hash32 [Hash32Length]byte

// Hash20 represents the 20-byte sha256 hash of arbitrary data.
type Hash20 [hash20Length]byte

// Field returns a log field. Implements the LoggableField interface.
func (h Hash12) Field() log.Field { return log.String("hash", util.Bytes2Hex(h[:])) }

// Bytes gets the byte representation of the underlying hash.
func (h Hash20) Bytes() []byte { return h[:] }

// Big converts a hash to a big integer.
func (h Hash20) Big() *big.Int { return new(big.Int).SetBytes(h[:]) }

// Hex converts a hash to a hex string.
func (h Hash20) Hex() string { return util.Encode(h[:]) }

// String implements the stringer interface and is used also by the logger when
// doing full logging into a file.
func (h Hash20) String() string {
	return h.Hex()
}

// ShortString returns a the first 5 characters of the hash, for logging purposes.
func (h Hash20) ShortString() string {
	l := len(h.Hex())
	return h.Hex()[util.Min(2, l):util.Min(7, l)]
}

// Format implements fmt.Formatter, forcing the byte slice to be formatted as is,
// without going through the stringer interface used for logging.
func (h Hash20) Format(s fmt.State, c rune) {
	_, _ = fmt.Fprintf(s, "%"+string(c), h[:])
}

// UnmarshalText parses a hash in hex syntax.
func (h *Hash20) UnmarshalText(input []byte) error {
	return util.UnmarshalFixedText("Hash", input, h[:])
}

// UnmarshalJSON parses a hash in hex syntax.
func (h *Hash20) UnmarshalJSON(input []byte) error {
	return util.UnmarshalFixedJSON(hashT, input, h[:])
}

// MarshalText returns the hex representation of h.
func (h Hash20) MarshalText() ([]byte, error) {
	return util.Bytes(h[:]).MarshalText()
}

// SetBytes sets the hash to the value of b.
// If b is larger than len(h), b will be cropped from the left.
func (h *Hash20) SetBytes(b []byte) {
	if len(b) > len(h) {
		b = b[len(b)-32:]
	}

	copy(h[32-len(b):], b)
}

// ToHash32 returns a Hash32 whose first 20 bytes are the bytes of this Hash20, it is right-padded with zeros.
func (h Hash20) ToHash32() (h32 Hash32) {
	copy(h32[:], h[:])
	return
}

// Field returns a log field. Implements the LoggableField interface.
func (h Hash20) Field() log.Field { return log.String("hash", util.Bytes2Hex(h[:])) }

// CalcHash12 returns the 12-byte prefix of the sha256 sum of the given byte slice.
func CalcHash12(data []byte) (h Hash12) {
	h32 := sha256.Sum256(data)
	copy(h[:], h32[:])
	return
}

// CalcBlocksHash12 returns the 12-byte sha256 sum of the block IDs, sorted in lexicographic order.
func CalcBlocksHash12(view []BlockID) (h Hash12) {
	h32 := CalcBlocksHash32(view, nil)
	copy(h[:], h32[:])
	return
}

// CalcBlocksHash32 returns the 32-byte sha256 sum of the block IDs, sorted in lexicographic order. The pre-image is
// prefixed with additionalBytes.
func CalcBlocksHash32(view []BlockID, additionalBytes []byte) Hash32 {
	sortedView := make([]BlockID, len(view))
	copy(sortedView, view)
	SortBlockIDs(sortedView)
	return CalcBlockHash32Presorted(sortedView, additionalBytes)
}

// CalcBlockHash32Presorted returns the 32-byte sha256 sum of the block IDs, in the order given. The pre-image is
// prefixed with additionalBytes.
func CalcBlockHash32Presorted(sortedView []BlockID, additionalBytes []byte) Hash32 {
	hash := sha256.New()
	hash.Write(additionalBytes)
	for _, id := range sortedView {
		hash.Write(id.Bytes()) // this never returns an error: https://golang.org/pkg/hash/#Hash
	}
	var res Hash32
	hash.Sum(res[:0])
	return res
}

// CalcMessageHash12 returns the 12-byte sha256 sum of the given msg suffixed with protocol.
func CalcMessageHash12(msg []byte, protocol string) Hash12 {
	return CalcHash12(append(msg, protocol...))
}

var hashT = reflect.TypeOf(Hash32{})

// CalcHash32 returns the 32-byte sha256 sum of the given data.
func CalcHash32(data []byte) Hash32 {
	return sha256.Sum256(data)
}

// CalcAggregateHash32 returns the 32-byte sha256 sum of the given data aggregated with previous hash h
func CalcAggregateHash32(h Hash32, data []byte) Hash32 {
	hash := sha256.New()
	hash.Write(h.Bytes())
	hash.Write(data) // this never returns an error: https://golang.org/pkg/hash/#Hash
	var res Hash32
	hash.Sum(res[:0])
	return res
}

// CalcATXHash32 returns the 32-byte sha256 sum of serialization of the given ATX.
func CalcATXHash32(atx *ActivationTx) Hash32 {
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

// HexToHash32 sets byte representation of s to hash.
// If b is larger than len(h), b will be cropped from the left.
func HexToHash32(s string) Hash32 { return BytesToHash(util.FromHex(s)) }

// Bytes gets the byte representation of the underlying hash.
func (h Hash32) Bytes() []byte { return h[:] }

// Hex converts a hash to a hex string.
func (h Hash32) Hex() string { return util.Encode(h[:]) }

// String implements the stringer interface and is used also by the logger when
// doing full logging into a file.
func (h Hash32) String() string {
	return h.Hex()
}

// ShortString returns the first 5 characters of the hash, for logging purposes.
func (h Hash32) ShortString() string {
	l := len(h.Hex())
	return Shorten(h.Hex()[util.Min(2, l):], 10)
}

// Shorten shortens a string to a specified length
func Shorten(s string, maxlen int) string {
	l := len(s)
	return s[:util.Min(maxlen, l)]
}

// Format implements fmt.Formatter, forcing the byte slice to be formatted as is,
// without going through the stringer interface used for logging.
func (h Hash32) Format(s fmt.State, c rune) {
	_, _ = fmt.Fprintf(s, "%"+string(c), h[:])
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

// ToHash20 returns a Hash20, whose the 20-byte prefix of this Hash32.
func (h Hash32) ToHash20() (h20 Hash20) {
	copy(h20[:], h[:])
	return
}

// Generate implements testing/quick.Generator.
func (h Hash32) Generate(rand *rand.Rand, _ int) reflect.Value {
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

// Field returns a log field. Implements the LoggableField interface.
func (h Hash32) Field() log.Field { return log.String("hash", util.Bytes2Hex(h[:])) }
