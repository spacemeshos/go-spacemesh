package codec

import (
	ssz "github.com/ferranbt/fastssz"
)

// Encodable is an interface that must be implemented by a struct to be encoded.
type Encodable = ssz.Marshaler

// Decodable is an interface that must be implemented bya struct to be decoded.
type Decodable = ssz.Unmarshaler

// Encode value to a byte buffer.
func Encode(value Encodable) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	buf := make([]byte, 0, value.SizeSSZ())
	return value.MarshalSSZTo(buf)
}

// Decode value from a byte buffer.
func Decode(buf []byte, value Decodable) error {
	return value.UnmarshalSSZ(buf)
}
