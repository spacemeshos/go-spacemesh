package types

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"iter"
	"slices"
)

const (
	// FingerprintSize is the size of a fingerprint in bytes.
	FingerprintSize = 12
)

// Seq represents an ordered sequence of elements.
// Each element which is yielded is paired with en error, which, if non-nil, indicates
// that an error has occurred while acquiring the next element, such as a database error.
// Unless the sequence is empty or an error occurs while iterating, it yields elements
// endlessly, wrapping around to the first element after the last one.
type Seq iter.Seq2[KeyBytes, error]

// First returns the first element from the sequence, if any.
// If the sequence is empty, it returns nil.
func (s Seq) First() (KeyBytes, error) {
	for k, err := range s {
		return k, err
	}
	return nil, nil
}

// GetN returns the first n elements from the sequence, converting them to the specified
// type T.
func GetN(s Seq, n int) ([]KeyBytes, error) {
	res := make([]KeyBytes, 0, n)
	for k, err := range s {
		if err != nil {
			return nil, err
		}
		res = append(res, k)
		if len(res) == n {
			break
		}
	}
	return res, nil
}

// EmptySeq returns an empty sequence.
func EmptySeq() Seq {
	return Seq(func(yield func(KeyBytes, error) bool) {})
}

// KeyBytes represents an item (key) in a reconciliable set.
type KeyBytes []byte

// String implements fmt.Stringer.
func (k KeyBytes) String() string {
	return hex.EncodeToString(k)
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
