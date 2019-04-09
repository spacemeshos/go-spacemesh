// Package xdr provides helper types and methods for XDR-based encoding and decoding.
package xdr

import (
	"io"
	"math/big"
	"reflect"

	xdr "github.com/stellar/go-xdr/xdr3"
)

// Marshal writes the XDR encoding of val to w.
func Marshal(w io.Writer, val interface{}) error {
	encoder := xdr.NewEncoder(w) // Initialize encoder

	realVal := getPtrValue(val) // Deserialize pointer

	if sliceValue := getBigSliceValue(realVal); sliceValue != nil { // Check big value can be encoded
		_, err := encoder.EncodeOpaque(sliceValue) // Encode

		if err != nil { // Check for errors
			return err // Return found error
		}

		return nil // No error occurred, return nil
	}

	_, err := encoder.Encode(realVal.Interface()) // Encode

	if err != nil { // Check for errors
		return err // Return found error
	}

	return nil // No error occurred, return nil
}

// getPtrValue fetches the value of a given pointer interface. If the value is not a pointer,
// the inputted value is returned.
func getPtrValue(val interface{}) reflect.Value {
	valBuffer := reflect.ValueOf(val) // Declare buffer to do pointer operations on

	for valBuffer.Kind() == reflect.Ptr { // Do until is not pointer
		valBuffer = valBuffer.Elem() // Set to value
	}

	return valBuffer // Return value buffer
}

// getBigSliceValue encodes a given reflect.Value to a slice of bytes. If the given
// value is not a big.Int or big.Float, nil is returned.
func getBigSliceValue(val reflect.Value) []byte {
	bigIntVal, isBigInt := val.Interface().(big.Int) // Get big int val

	if isBigInt { // Is big int
		return bigIntVal.Bytes() // Return byte slice encoding
	}

	bigFloatVal, isBigFloat := val.Interface().(big.Int) // Get big float val

	if isBigFloat { // Is big float
		return bigFloatVal.Bytes() // Set to big float byte encoding
	}

	return nil // Not big float or int
}
