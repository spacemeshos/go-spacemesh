package util

import (
	"encoding/binary"
)

func BytesToUint32(i []byte) uint32 { return binary.LittleEndian.Uint32(i) }

func Uint32ToBytes(i uint32) []byte {
	a := make([]byte, 4)
	binary.LittleEndian.PutUint32(a, i)
	return a
}

func BytesToUint64(i []byte) uint64 { return binary.LittleEndian.Uint64(i) }

func Uint64ToBytes(i uint64) []byte {
	a := make([]byte, 8)
	binary.LittleEndian.PutUint64(a, i)
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
