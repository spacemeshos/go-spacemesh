package rangesync

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
)

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
