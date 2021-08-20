package codec

import (
	"io"

	ssz "github.com/ferranbt/fastssz"
	xdr "github.com/nullstyle/go-xdr/xdr3"
)

// Encodable is an interface that must be implemented by a struct to be encoded.
type Encodable = ssz.Marshaler

// Decodable is an interface that must be implemented bya struct to be decoded.
type Decodable = ssz.Unmarshaler

// EncodeTo encodes value to a writer stream.
func EncodeTo(w io.Writer, value Encodable) (int, error) {
	return xdr.Marshal(w, value)
}

// DecodeFrom decodes a value using data from a reader stream.
func DecodeFrom(r io.Reader, value Decodable) (int, error) {
	return xdr.Unmarshal(r, value)
}

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
