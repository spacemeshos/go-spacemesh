// Package xdr provides helper types and methods for XDR-based encoding and decoding.
package xdr

import (
	"io"
	"math/big"

	xdr "github.com/dowlandaiello/go-xdr/xdr3"
)

// Unmarshal unmarshals data read from reader r into the interface buffer val.
// Returns the number of bytes decoded.
func Unmarshal(r io.Reader, val interface{}) (int, error) {
	decoder := xdr.NewDecoder(r) // Initialize decoder

	bigValueType, isBigValue := isBigValue(val) // Check is decoding into big value

	if isBigValue { // Check is big value
		val = []byte{} // Set to empty byte array
	}

	n, err := decoder.Decode(val) // Decode

	if err != nil { // Check for errors
		return n, err // Return found error
	}

	if isBigValue { // Check is decoding into big value
		switch bigValueType { // Switch value
		case 0:
			bigIntBuffer := new(big.Int) // Initialize big int

			bigIntBuffer.SetBytes(val.([]byte)) // Decode bytes

			if isPtr := getPtrValue(val); isPtr != val { // Check is pointer
				val = bigIntBuffer // Set value

				return n, nil // Return decoded bytes
			}

			val = *bigIntBuffer // Set value

			return n, nil // Return decoded bytes
		case 1:
			bigFloatBuffer := new(big.Float) // Initialize big float

			bigFloatBuffer.SetString(string(val.([]byte))) // Decode string value

			if isPtr := getPtrValue(val); isPtr != val { // Check is pointer
				val = bigFloatBuffer // Set value

				return n, nil // Return decoded bytes
			}

			val = *bigFloatBuffer // Set value

			return n, nil // Return decoded bytes
		}
	}

	return decoder.Decode(val) // Decode
}
