// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package util

/*
Encoding Rules

All hex data must have prefix "0x".

For byte slices, the hex data must be of even length. An empty byte slice
encodes as "0x".

Integers are encoded using the least amount of digits (no leading zero digits). Their
encoding may be of uneven length. The number zero encodes as "0x0".
*/

import (
	"encoding/hex"
	"errors"
)

// Errors.
var (
	ErrSyntax        = errors.New("invalid hex string")
	ErrMissingPrefix = errors.New("hex string without 0x prefix")
	ErrOddLength     = errors.New("hex string of odd length")
	ErrUint64Range   = errors.New("hex number > 64 bits")
)

// Encode encodes b as a hex string with 0x prefix.
func Encode(b []byte) string {
	enc := make([]byte, len(b)*2+2)
	copy(enc, "0x")
	hex.Encode(enc[2:], b)
	return string(enc)
}

// Hex2Bytes returns the bytes represented by the hexadecimal string str.
// Note that str should not be "0x" prefixed. To decode a "0x" prefixed string use FromHex.
func Hex2Bytes(str string) []byte {
	h, _ := hex.DecodeString(str)
	return h
}

// FromHex returns the bytes represented by the hexadecimal string s.
// Parameter s may be prefixed with "0x".
func FromHex(s string) []byte {
	if len(s) > 1 {
		if s[0:2] == "0x" || s[0:2] == "0X" {
			s = s[2:]
		}
	}
	if len(s)%2 == 1 {
		s = "0" + s
	}
	return Hex2Bytes(s)
}
