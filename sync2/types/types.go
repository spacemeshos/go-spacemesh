package types

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"iter"
	"slices"
)

const (
	FingerprintSize = 12
)

type Ordered interface {
	Compare(other any) int
}

// Seq represents an ordered sequence of elements.
// Each element which is yielded is paired with en error, which, if non-nil, indicates
// that an error has occurred while acquiring the next element, such as a database error.
// Unless the sequence is empty or an error occurs while iterating, it yields elements
// endlessly, wrapping around to the first element after the last one.
type Seq iter.Seq2[Ordered, error]

// First returns the first element from the sequence, if any.
// If the sequence is empty, it returns nil.
func (s Seq) First() (Ordered, error) {
	for k, err := range s {
		return k, err
	}
	return nil, nil
}

// GetN returns the first n elements from the sequence, converting them to the specified
// type T.
func GetN[T Ordered](s Seq, n int) ([]T, error) {
	res := make([]T, 0, n)
	for k, err := range s {
		if err != nil {
			return nil, err
		}
		res = append(res, k.(T))
		if len(res) == n {
			break
		}
	}
	return res, nil
}

func EmptySeq() Seq {
	return Seq(func(yield func(Ordered, error) bool) {})
}

type KeyBytes []byte

func (k KeyBytes) String() string {
	return hex.EncodeToString(k)
}

func (k KeyBytes) Clone() KeyBytes {
	return slices.Clone(k)
}

func (k KeyBytes) Compare(other any) int {
	return bytes.Compare(k, other.(KeyBytes))
}

func (k KeyBytes) Inc() (overflow bool) {
	for i := len(k) - 1; i >= 0; i-- {
		k[i]++
		if k[i] != 0 {
			return false
		}
	}

	return true
}

func (k KeyBytes) Zero() {
	for i := range k {
		k[i] = 0
	}
}

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

func HexToKeyBytes(s string) KeyBytes {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("bad hex key bytes: " + err.Error())
	}
	return KeyBytes(b)
}

type Fingerprint [FingerprintSize]byte

func RandomFingerprint() Fingerprint {
	var fp Fingerprint
	_, err := rand.Read(fp[:])
	if err != nil {
		panic("failed to generate random fingerprint: " + err.Error())
	}
	return fp
}

func EmptyFingerprint() Fingerprint {
	return Fingerprint{}
}

func (fp Fingerprint) Compare(other Fingerprint) int {
	return bytes.Compare(fp[:], other[:])
}

func (fp Fingerprint) String() string {
	return hex.EncodeToString(fp[:])
}

func (fp *Fingerprint) Update(h []byte) {
	for n := range *fp {
		(*fp)[n] ^= h[n]
	}
}

func (fp *Fingerprint) BitFromLeft(n int) bool {
	if n > FingerprintSize*8 {
		panic("BUG: bad fingerprint bit index")
	}
	return (fp[n>>3]>>(7-n&0x7))&1 != 0
}

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
