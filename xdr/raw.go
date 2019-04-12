// Package xdr provides helper types and methods for XDR-based encoding and decoding.
package xdr

// RawValue represents an encoded XDR value and can be used to delay
// XDR decoding or to precompute an encoding. Note that the decoder does
// not verify whether the content of RawValues is valid XDR.
type RawValue []byte
