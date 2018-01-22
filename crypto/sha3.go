package crypto

import (
	"golang.org/x/crypto/sha3"
)

// SHA-3-256 (not sha-256) hashing
// Returns a 32 bytes (256 bits) hash of the data
func Sha256(data ...[]byte) []byte {
	d := sha3.New256()
	for _, b := range data {
		d.Write(b)
	}
	return d.Sum(nil)
}
