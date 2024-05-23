package types

import (
	"encoding/hex"
	"fmt"
	"reflect"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	Hash32Length = 32
	Hash20Length = 20
)

var (
	hash20T = reflect.TypeOf(Hash20{})
	hash32T = reflect.TypeOf(Hash32{})

	// EmptyHash32 is the zero hash.
	EmptyHash32 = Hash32{}
)

// Hash32 represents the 32-byte blake3 hash of arbitrary data.
type Hash32 [Hash32Length]byte

// Hash20 represents the 20-byte blake3 hash of arbitrary data.
type Hash20 [Hash20Length]byte

// Bytes gets the byte representation of the underlying hash.
func (h Hash20) Bytes() []byte { return h[:] }

// String implements the stringer interface and is used also by the logger when
// doing full logging into a file.
func (h Hash20) String() string {
	return hex.EncodeToString(h[:])
}

// ShortString returns a the first 5 hex-encoded bytes of the hash, for logging purposes.
func (h Hash20) ShortString() string {
	return hex.EncodeToString(h[:5])
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
	if err := util.UnmarshalFixedJSON(hash20T, input, h[:]); err != nil {
		return fmt.Errorf("unmarshal JSON: %w", err)
	}

	return nil
}

// MarshalText returns the hex representation of h.
func (h Hash20) MarshalText() ([]byte, error) {
	return util.Bytes(h[:]).MarshalText()
}

// SetBytes sets the hash to the value of b.
// If b is larger than len(h), b will be cropped from the left.
func (h *Hash20) SetBytes(b []byte) {
	if len(b) > len(h) {
		b = b[len(b)-20:]
	}

	copy(h[20-len(b):], b)
}

// ToHash32 returns a Hash32 whose first 20 bytes are the bytes of this Hash20, it is right-padded with zeros.
func (h Hash20) ToHash32() (h32 Hash32) {
	copy(h32[:], h[:])
	return
}

// CalcProposalsHash32 returns the 32-byte blake3 sum of the IDs, sorted in lexicographic order. The pre-image is
// prefixed with additionalBytes.
func CalcProposalsHash32(view []ProposalID, additionalBytes []byte) Hash32 {
	sortedView := make([]ProposalID, len(view))
	copy(sortedView, view)
	SortProposalIDs(sortedView)
	return CalcProposalHash32Presorted(sortedView, additionalBytes)
}

// CalcProposalHash32Presorted returns the 32-byte blake3 sum of the IDs, in the order given. The pre-image is
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

// CalcBlockHash32Presorted returns the 32-byte blake3 sum of the IDs, in the order given. The pre-image is
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

// CalcHash20 returns the 20-byte blake3 sum of the given data.
func CalcHash20(data []byte) Hash20 {
	return hash.Sum20(data)
}

// CalcHash32 returns the 32-byte blake3 sum of the given data.
func CalcHash32(data []byte) Hash32 {
	return hash.Sum(data)
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

// String implements the stringer interface and is used also by the logger when
// doing full logging into a file.
func (h Hash32) String() string {
	return hex.EncodeToString(h[:])
}

// ShortString returns the first 5 hex-encoded bytes of the hash, for logging purposes.
func (h Hash32) ShortString() string {
	return hex.EncodeToString(h[:5])
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
	if err := util.UnmarshalFixedJSON(hash32T, input, h[:]); err != nil {
		return fmt.Errorf("unmarshal JSON: %w", err)
	}

	return nil
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

// Field returns a log field. Implements the LoggableField interface.
func (h Hash20) Field() log.Field {
	return log.Stringer("hash", h)
}

// Field returns a log field. Implements the LoggableField interface.
func (h Hash32) Field() log.Field {
	return log.Stringer("hash", h)
}

// EncodeScale implements scale codec interface.
func (h *Hash32) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(e, h[:])
}

// DecodeScale implements scale codec interface.
func (h *Hash32) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, h[:])
}
