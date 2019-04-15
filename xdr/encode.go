// Package xdr provides helper types and methods for XDR-based encoding and decoding.
package xdr

import (
	"io"
	"math/big"
	"reflect"

	xdr "github.com/dowlandaiello/go-xdr/xdr3"
)

// Marshal writes the XDR encoding of val to w.
// Returns the number of bytes, n, written to writer w.
func Marshal(w io.Writer, val interface{}) (int, error) {
	encoder := xdr.NewEncoder(w) // Initialize encoder

	realVal := getPtrValue(val) // Deserialize pointer

	if realVal.Kind() == reflect.Chan || realVal.Kind() == reflect.Func || realVal.Kind() == reflect.Interface || realVal.Kind() == reflect.Map || realVal.Kind() == reflect.Ptr || realVal.Kind() == reflect.Slice && realVal.IsNil() || !realVal.IsValid() { // Check no value
		realVal = reflect.ValueOf([]byte{}) // Set to nil byte array
	}

	if realVal.Kind() == reflect.Interface && (realVal.IsNil() || !realVal.CanInterface()) { // Check is nil
		realVal = reflect.ValueOf([]byte{}) // Set to nil byte array
	}

	if sliceValue := getBigSliceValue(realVal); sliceValue != nil { // Check big value can be encoded
		_, err := encoder.EncodeOpaque(sliceValue) // Encode

		if err != nil { // Check for errors
			return 0, err // Return found error
		}

		return 0, nil // No error occurred, return nil
	}

	_, err := encoder.Encode(realVal.Interface()) // Encode

	if err != nil { // Check for errors
		return 0, err // Return found error
	}

	return 0, nil // No error occurred, return nil
}

// isBigValue determines if a given value is a *big.Int, big.Int, *big.Float, or big.Float.
func isBigValue(val interface{}) (int, bool) {
	realVal := getPtrValue(val) // Dereference pointer

	_, isBigInt := realVal.Interface().(big.Int)     // Check is bigInt
	_, isBigFloat := realVal.Interface().(big.Float) // Check is bigInt

	if isBigInt { // Check is big int
		return 0, true // Is big value
	} else if isBigFloat { // Check is big float
		return 1, true // Is big value
	}

	return -1, false // Not big value
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

	bigFloatVal, isBigFloat := val.Interface().(big.Float) // Get big float val

	if isBigFloat { // Is big float
		return []byte(bigFloatVal.String()) // Set to big float byte encoding
	}

	return nil // Not big float or int
}
