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

// BytesToUint64 interprets a byte slice as uint64, using little endian encoding.
func BytesToUint64(i []byte) uint64 { return binary.LittleEndian.Uint64(i) }

// Uint64ToBytes returns the byte representation of a uint64, using little endian encoding.
func Uint64ToBytes(i uint64) []byte {
	a := make([]byte, 8)
	binary.LittleEndian.PutUint64(a, i)
	return a
}

// Uint64ToBytesBigEndian returns the byte representation of a uint64, using big endian encoding (which is not the
// default for spacemesh).
func Uint64ToBytesBigEndian(i uint64) []byte {
	a := make([]byte, 8)
	binary.BigEndian.PutUint64(a, i)
	return a
}

// CopyBytes returns an exact copy of the provided bytes.
func CopyBytes(b []byte) (copiedBytes []byte) {
	if b == nil {
		return nil
	}
	copiedBytes = make([]byte, len(b))
	copy(copiedBytes, b)

	return
}

// LeftPadBytes zero-pads slice to the left up to length l.
func LeftPadBytes(slice []byte, l int) []byte {
	if l <= len(slice) {
		return slice
	}

	padded := make([]byte, l)
	copy(padded[l-len(slice):], slice)

	return padded
}
