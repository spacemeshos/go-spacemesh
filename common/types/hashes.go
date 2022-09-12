package types

import (
	"fmt"
	"math/big"
	"reflect"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	// Hash32Length is 32, the expected length of the hash.
	Hash32Length = 32
	hash20Length = 20
	hash12Length = 12
)

// Hash12 represents the first 12 bytes of sha256, mostly used for internal caches.
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
	if err := util.UnmarshalFixedText("Hash", input, h[:]); err != nil {
		return fmt.Errorf("unmarshal text: %w", err)
	}
	return nil
}

// UnmarshalJSON parses a hash in hex syntax.
func (h *Hash20) UnmarshalJSON(input []byte) error {
	if err := util.UnmarshalFixedJSON(hashT, input, h[:]); err != nil {
		return fmt.Errorf("unmarshal JSON: %w", err)
	}

	return nil
}

// MarshalText returns the hex representation of h.
func (h Hash20) MarshalText() ([]byte, error) {
	data, err := util.Bytes(h[:]).MarshalText()
	if err != nil {
		return data, fmt.Errorf("marshal text: %w", err)
	}

	return data, nil
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
	h32 := hash.Sum(data)
	copy(h[:], h32[:])
	return
}

// DecodeScale implements scale codec interface.
func (h *Hash20) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, h[:])
}

// CalcProposalsHash32 returns the 32-byte sha256 sum of the IDs, sorted in lexicographic order. The pre-image is
// prefixed with additionalBytes.
func CalcProposalsHash32(view []ProposalID, additionalBytes []byte) Hash32 {
	sortedView := make([]ProposalID, len(view))
	copy(sortedView, view)
	SortProposalIDs(sortedView)
	return CalcProposalHash32Presorted(sortedView, additionalBytes)
}

// CalcBlocksHash32 returns the 32-byte sha256 sum of the IDs, sorted in lexicographic order. The pre-image is
// prefixed with additionalBytes.
func CalcBlocksHash32(view []BlockID, additionalBytes []byte) Hash32 {
	sortedView := make([]BlockID, len(view))
	copy(sortedView, view)
	SortBlockIDs(sortedView)
	return CalcBlockHash32Presorted(sortedView, additionalBytes)
}

// CalcProposalHash32Presorted returns the 32-byte sha256 sum of the IDs, in the order given. The pre-image is
// prefixed with additionalBytes.
func CalcProposalHash32Presorted(sortedView []ProposalID, additionalBytes []byte) Hash32 {
	hasher := hash.New()
	hasher.Write(additionalBytes)
	for _, id := range sortedView {
		hasher.Write(id.Bytes()) // this never returns an error: https://golang.org/pkg/hash/#Hash
	}
	var res Hash32
	hasher.Sum(res[:0])
	return res
}

// CalcBlockHash32Presorted returns the 32-byte sha256 sum of the IDs, in the order given. The pre-image is
// prefixed with additionalBytes.
func CalcBlockHash32Presorted(sortedView []BlockID, additionalBytes []byte) Hash32 {
	hash := hash.New()
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
	return hash.Sum(data)
}

// CalcATXHash32 returns the 32-byte sha256 sum of serialization of the given ATX.
func CalcATXHash32(atx *ActivationTx) Hash32 {
	bytes, err := codec.Encode(atx)
	if err != nil {
		panic("could not serialize ATX")
	}
	return CalcHash32(bytes)
}

// CalcProposalHash32 returns the 32-byte sha256 sum of serialization of the given Proposal.
func CalcProposalHash32(proposal *Proposal) Hash32 {
	bytes, err := codec.Encode(proposal)
	if err != nil {
		panic("could not serialize Proposal")
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

// Shorten shortens a string to a specified length.
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
	if err := util.UnmarshalFixedText("Hash", input, h[:]); err != nil {
		return fmt.Errorf("unmarshal text: %w", err)
	}

	return nil
}

// UnmarshalJSON parses a hash in hex syntax.
func (h *Hash32) UnmarshalJSON(input []byte) error {
	if err := util.UnmarshalFixedJSON(hashT, input, h[:]); err != nil {
		return fmt.Errorf("unmarshal JSON: %w", err)
	}

	return nil
}

// MarshalText returns the hex representation of h.
func (h Hash32) MarshalText() ([]byte, error) {
	data, err := util.Bytes(h[:]).MarshalText()
	if err != nil {
		return data, fmt.Errorf("marshal text: %w", err)
	}

	return data, nil
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

// Field returns a log field. Implements the LoggableField interface.
func (h Hash32) Field() log.Field { return log.String("hash", util.Bytes2Hex(h[:])) }

// EncodeScale implements scale codec interface.
func (h *Hash32) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(e, h[:])
}

// DecodeScale implements scale codec interface.
func (h *Hash32) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, h[:])
}
