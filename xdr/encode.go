// Package xdr provides helper types and methods for XDR-based encoding and decoding.
package xdr

import (
	"io"

	xdr "github.com/stellar/go-xdr/xdr2"
)

// Encode writes the XDR encoding of val to w.
func Encode(w io.Writer, val interface{}) error {
	_, err := xdr.Marshal(w, val) // Encode

	if err != nil { // Check for errors
		return err // Return found error
	}

	return nil // No error occurred, return nil
}
