// Package util provides common utility functions.
package util

import (
	"encoding/binary"
)

// Uint64ToBytesBigEndian returns the byte representation of a uint64, using big endian encoding (which is not the
// default for spacemesh).
func Uint64ToBytesBigEndian(i uint64) []byte {
	a := make([]byte, 8)
	binary.BigEndian.PutUint64(a, i)
	return a
}
