package crypto

import (
	"golang.org/x/crypto/sha3"
)

// Sha256 is a SHA-3-256 (not sha-256) hasher. It returns a 32 bytes (256 bits) hash of data.
// data: arbitrary length bytes slice
func Sha256(data ...[]byte) []byte {
	d := sha3.New256()
	for _, b := range data {
		d.Write(b)
	}
	return d.Sum(nil)
}
