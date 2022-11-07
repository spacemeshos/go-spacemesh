// Package util provides common utility functions.
package util

import (
	"encoding/binary"
)

// BytesToUint32 interprets a byte slice as uint32, using little endian encoding.
func BytesToUint32(i []byte) uint32 { return binary.LittleEndian.Uint32(i) }

// Uint32ToBytes returns the byte representation of a uint32, using little endian encoding.
func Uint32ToBytes(i uint32) []byte {
	a := make([]byte, 4)
	binary.LittleEndian.PutUint32(a, i)
	return a
}

// Uint64ToBytesBigEndian returns the byte representation of a uint64, using big endian encoding (which is not the
// default for spacemesh).
func Uint64ToBytesBigEndian(i uint64) []byte {
	a := make([]byte, 8)
	binary.BigEndian.PutUint64(a, i)
	return a
}
