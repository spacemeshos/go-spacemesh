package types

import (
	"io"

	xdr "github.com/nullstyle/go-xdr/xdr3"
)

// Encode ...
func Encode(w io.Writer, value interface{}) (int, error) {
	return xdr.Marshal(w, value)
}

// Decode ...
func Decode(r io.Reader, value interface{}) (int, error) {
	return xdr.Unmarshal(r, value)
}
