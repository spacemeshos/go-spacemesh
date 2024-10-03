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
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func referenceBytes(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

var unmarshalBytesErrorTests = []unmarshalTest{
	// invalid encoding
	{input: "", wantErr: errors.New("unexpected end of JSON input")},
	{input: "null", wantErr: errNonString(bytesT)},
	{input: "10", wantErr: errNonString(bytesT)},
	{input: `"0"`, wantErr: wrapTypeError(ErrMissingPrefix, bytesT)},
	{input: `"0x0"`, wantErr: wrapTypeError(ErrOddLength, bytesT)},
	{input: `"0xxx"`, wantErr: wrapTypeError(ErrSyntax, bytesT)},
	{input: `"0x01zz01"`, wantErr: wrapTypeError(ErrSyntax, bytesT)},
}

var unmarshalBytesTests = []unmarshalTest{
	// valid encoding
	{input: `""`, want: referenceBytes("")},
	{input: `"0x"`, want: referenceBytes("")},
	{input: `"0x02"`, want: referenceBytes("02")},
	{input: `"0X02"`, want: referenceBytes("02")},
	{input: `"0xffffffffff"`, want: referenceBytes("ffffffffff")},
	{
		input: `"0xffffffffffffffffffffffffffffffffffff"`,
		want:  referenceBytes("ffffffffffffffffffffffffffffffffffff"),
	},
}

func TestUnmarshalBytes(t *testing.T) {
	for _, test := range unmarshalBytesErrorTests {
		var v Bytes
		err := json.Unmarshal([]byte(test.input), &v)
		require.EqualError(t, err, test.wantErr.Error())
	}

	for _, test := range unmarshalBytesTests {
		var v Bytes
		err := json.Unmarshal([]byte(test.input), &v)
		require.NoError(t, err)
		require.Equal(t, test.want.([]byte), []byte(v))
	}
}

func BenchmarkUnmarshalBytes(b *testing.B) {
	input := []byte(`"0x123456789abcdef123456789abcdef"`)
	for i := 0; i < b.N; i++ {
		var v Bytes
		if err := v.UnmarshalJSON(input); err != nil {
			b.Fatal(err)
		}
	}
}

func TestMarshalBytes(t *testing.T) {
	for _, test := range encodeBytesTests {
		in := test.input.([]byte)
		out, err := json.Marshal(Bytes(in))
		require.NoError(t, err)
		require.Equal(t, strconv.Quote(test.want), string(out))
		require.Equal(t, test.want, Bytes(in).String())
	}
}
