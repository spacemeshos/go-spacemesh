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
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
)

func checkError(t *testing.T, input string, got, want error) bool {
	if got == nil {
		if want != nil {
			t.Errorf("input %s: got no error, want %q", input, want)
			return false
		}
		return true
	}
	if want == nil {
		t.Errorf("input %s: unexpected error %q", input, got)
	} else if got.Error() != want.Error() {
		t.Errorf("input %s: got error %q, want %q", input, got, want)
	}
	return false
}

func referenceBytes(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

var errJSONEOF = errors.New("unexpected end of JSON input")

var unmarshalBytesTests = []unmarshalTest{
	// invalid encoding
	{input: "", wantErr: errJSONEOF},
	{input: "null", wantErr: errNonString(bytesT)},
	{input: "10", wantErr: errNonString(bytesT)},
	{input: `"0"`, wantErr: wrapTypeError(ErrMissingPrefix, bytesT)},
	{input: `"0x0"`, wantErr: wrapTypeError(ErrOddLength, bytesT)},
	{input: `"0xxx"`, wantErr: wrapTypeError(ErrSyntax, bytesT)},
	{input: `"0x01zz01"`, wantErr: wrapTypeError(ErrSyntax, bytesT)},

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
	for _, test := range unmarshalBytesTests {
		var v Bytes
		err := json.Unmarshal([]byte(test.input), &v)
		if !checkError(t, test.input, err, test.wantErr) {
			continue
		}
		if !bytes.Equal(test.want.([]byte), []byte(v)) {
			t.Errorf("input %s: value mismatch: got %x, want %x", test.input, &v, test.want)
			continue
		}
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
		if err != nil {
			t.Errorf("%x: %v", in, err)
			continue
		}
		if want := `"` + test.want + `"`; string(out) != want {
			t.Errorf("%x: MarshalJSON output mismatch: got %q, want %q", in, out, want)
			continue
		}
		if out := Bytes(in).String(); out != test.want {
			t.Errorf("%x: String mismatch: got %q, want %q", in, out, test.want)
			continue
		}
	}
}
