package types

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"iter"
	"slices"

	"go.uber.org/zap/zapcore"
)

const (
	// FingerprintSize is the size of a fingerprint in bytes.
	FingerprintSize = 12
)

// Seq represents an ordered sequence of elements.
// Unless the sequence is empty or an error occurs while iterating, it yields elements
// endlessly, wrapping around to the first element after the last one.
type Seq iter.Seq[KeyBytes]

var _ zapcore.ArrayMarshaler = Seq(nil)

// First returns the first element from the sequence, if any.
// If the sequence is empty, it returns nil.
func (s Seq) First() KeyBytes {
	for k := range s {
		return k
	}
	return nil
}

// GetN returns the first n elements from the sequence.
func (s Seq) GetN(n int) []KeyBytes {
	res := make([]KeyBytes, 0, n)
	for k := range s {
		if len(res) == n {
			break
		}
		res = append(res, k)
	}
	return res
}

// MarshalLogArray implements zapcore.ArrayMarshaler.
func (s Seq) MarshalLogArray(enc zapcore.ArrayEncoder) error {
	if s == nil {
		return nil
	}
	n := 0
	for k := range s {
		if n == 3 {
			enc.AppendString("...")
			break
		}
		enc.AppendString(k.ShortString())
		n++
	}
	return nil
}

// EmptySeq returns an empty sequence.
func EmptySeq() Seq {
	return Seq(func(yield func(KeyBytes) bool) {})
}

// SeqErrorFunc is a function that returns an error that happened during iteration, if
// any.
type SeqErrorFunc func() error

// NoSeqError is a SeqErrorFunc that always returns nil (no error).
var NoSeqError SeqErrorFunc = func() error { return nil }

// SeqError returns a SeqErrorFunc that always returns the given error.
func SeqError(err error) SeqErrorFunc {
	return func() error { return err }
}

// SeqResult represents the result of a function that returns a sequence.
// Error method most be called to check if an error occurred after
// processing the sequence.
// Error is reset at the beginning of each Seq call (iteration over the sequence).
type SeqResult struct {
	Seq   Seq
	Error SeqErrorFunc
}

// MarshalLogArray implements zapcore.ArrayMarshaler.
func (s SeqResult) MarshalLogArray(enc zapcore.ArrayEncoder) error {
	s.Seq.MarshalLogArray(enc) // never returns an error
	return s.Error()
}

// First returns the first element from the result's sequence, if any.
// If the sequence is empty, it returns nil.
func (s SeqResult) First() (KeyBytes, error) {
	var r KeyBytes
	for r = range s.Seq {
		break
	}
	return r, s.Error()
}

// GetN returns the first n elements from the result's sequence.
func (s SeqResult) GetN(n int) ([]KeyBytes, error) {
	items := s.Seq.GetN(n)
	return items, s.Error()
}

// EmptySeqResult returns an empty sequence result.
func EmptySeqResult() SeqResult {
	return SeqResult{
		Seq:   EmptySeq(),
		Error: func() error { return nil },
	}
}

// ErrorSeqResult returns a sequence result with an empty sequence and an error.
func ErrorSeqResult(err error) SeqResult {
	return SeqResult{
		Seq:   EmptySeq(),
		Error: SeqError(err),
	}
}

// KeyBytes represents an item (key) in a reconciliable set.
type KeyBytes []byte

// String implements fmt.Stringer.
func (k KeyBytes) String() string {
	return hex.EncodeToString(k)
}

// String implements log.ShortString.
func (k KeyBytes) ShortString() string {
	if len(k) < 5 {
		return k.String()
	}
	return hex.EncodeToString(k[:5])
}

// Clone returns a copy of the key.
func (k KeyBytes) Clone() KeyBytes {
	return slices.Clone(k)
}

// Compare compares two keys.
func (k KeyBytes) Compare(other any) int {
	return bytes.Compare(k, other.(KeyBytes))
}

// Inc returns the key with the same number of bytes as this one, obtained by incrementing
// the key by one. It returns true if the increment has caused an overflow.
func (k KeyBytes) Inc() (overflow bool) {
	for i := len(k) - 1; i >= 0; i-- {
		k[i]++
		if k[i] != 0 {
			return false
		}
	}

	return true
}

// Zero sets all bytes in the key to zero.
func (k KeyBytes) Zero() {
	for i := range k {
		k[i] = 0
	}
}

// IsZero returns true if all bytes in the key are zero.
func (k KeyBytes) IsZero() bool {
	for _, b := range k {
		if b != 0 {
			return false
		}
	}
	return true
}

// RandomKeyBytes generates random data in bytes for testing.
func RandomKeyBytes(size int) KeyBytes {
	b := make([]byte, size)
	_, err := rand.Read(b)
	if err != nil {
		return nil
	}
	return b
}

// HexToKeyBytes converts a hex string to KeyBytes.
func HexToKeyBytes(s string) KeyBytes {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("bad hex key bytes: " + err.Error())
	}
	return KeyBytes(b)
}

// Fingerprint represents a fingerprint of a set of keys.
// The fingerprint is obtained by XORing together the keys in the set.
type Fingerprint [FingerprintSize]byte

// String implements log.ShortString.
func (fp Fingerprint) ShortString() string {
	return hex.EncodeToString(fp[:5])
}

// Compare compares two fingerprints.
func (fp Fingerprint) Compare(other Fingerprint) int {
	return bytes.Compare(fp[:], other[:])
}

// String implements fmt.Stringer.
func (fp Fingerprint) String() string {
	return hex.EncodeToString(fp[:])
}

// Update includes the byte slice in the fingerprint.
func (fp *Fingerprint) Update(h []byte) {
	for n := range *fp {
		(*fp)[n] ^= h[n]
	}
}

// BitFromLeft returns the n-th bit from the left in the fingerprint.
func (fp *Fingerprint) BitFromLeft(n int) bool {
	if n > FingerprintSize*8 {
		panic("BUG: bad fingerprint bit index")
	}
	return (fp[n>>3]>>(7-n&0x7))&1 != 0
}

// RandomFingerprint generates a random fingerprint.
func RandomFingerprint() Fingerprint {
	var fp Fingerprint
	_, err := rand.Read(fp[:])
	if err != nil {
		panic("failed to generate random fingerprint: " + err.Error())
	}
	return fp
}

// EmptyFingerprint returns an empty fingerprint.
func EmptyFingerprint() Fingerprint {
	return Fingerprint{}
}

// HexToFingerprint converts a hex string to Fingerprint.
func HexToFingerprint(s string) Fingerprint {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("bad hex fingerprint: " + err.Error())
	}
	var fp Fingerprint
	if len(b) != len(fp) {
		panic("bad hex fingerprint")
	}
	copy(fp[:], b)
	return fp
}
