// Package util provides common utility functions.
package util

import (
	"encoding/binary"
	"fmt"
)

// BytesToUint32 interprets a byte slice as uint32, using little endian encoding.
func BytesToUint32(i []byte) uint32 {
	switch len(i) {
	case 0:
		return 0
	case 1:
		return uint32(i[0])
	case 2:
		return uint32(i[0]) | uint32(i[1])<<8
	case 3:
		return uint32(i[0]) | uint32(i[1])<<8 | uint32(i[2])<<16
	default:
		return uint32(i[0]) | uint32(i[1])<<8 | uint32(i[2])<<16 | uint32(i[3])<<24
	}
}

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

// Uint32ToBytesBE marshal uint32 to bytes in big endian format.
func Uint32ToBytesBE(i uint32) []byte {
	a := make([]byte, 4)
	binary.BigEndian.PutUint32(a, i)
	return a
}

// BytesToUint32BE decode buf as uint32. Panics if length is not 4 bytes.
func BytesToUint32BE(buf []byte) uint32 {
	if len(buf) != 4 {
		panic(fmt.Sprintf("invalid buffer to decode uint32 %v", buf))
	}
	return binary.BigEndian.Uint32(buf)
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
