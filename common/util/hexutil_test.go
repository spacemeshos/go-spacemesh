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

import (
	"testing"
)

type marshalTest struct {
	input any
	want  string
}

type unmarshalTest struct {
	input   string
	want    any
	wantErr error // if set, decoding must fail on any platform
}

var encodeBytesTests = []marshalTest{
	{[]byte{}, "0x"},
	{[]byte{0}, "0x00"},
	{[]byte{0, 0, 1, 2}, "0x00000102"},
}

func TestEncode(t *testing.T) {
	for _, test := range encodeBytesTests {
		enc := Encode(test.input.([]byte))
		if enc != test.want {
			t.Errorf("input %x: wrong encoding %s", test.input, enc)
		}
	}
}
