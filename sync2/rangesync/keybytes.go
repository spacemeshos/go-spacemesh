package rangesync

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"slices"
)

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
func (k KeyBytes) Compare(other KeyBytes) int {
	return bytes.Compare(k, other)
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

// MustParseHexKeyBytes converts a hex string to KeyBytes.
func MustParseHexKeyBytes(s string) KeyBytes {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("bad hex key bytes: " + err.Error())
	}
	return KeyBytes(b)
}
