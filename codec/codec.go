package codec

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	xdr "github.com/nullstyle/go-xdr/xdr3"
)

func init() {
	// xdr will fail with overflow if slice size is larger than 1mb
	// see BenchmarkInvalidLength
	xdr.SliceLimit = 1 << 20
}

// Encodable is an interface that must be implemented by a struct to be encoded.
type Encodable interface{}

// Decodable is an interface that must be implemented bya struct to be decoded.
type Decodable interface{}

// EncodeTo encodes value to a writer stream.
func EncodeTo(w io.Writer, value Encodable) (int, error) {
	v, err := xdr.Marshal(w, value)
	if err != nil {
		return v, fmt.Errorf("marshal XDR: %w", err)
	}

	return v, nil
}

// DecodeFrom decodes a value using data from a reader stream.
func DecodeFrom(r io.Reader, value Decodable) (int, error) {
	v, err := xdr.Unmarshal(r, value)
	if err != nil {
		return v, fmt.Errorf("unmarshal XDR: %w", err)
	}

	return v, nil
}

// TODO(dshulyak) this is a temporary solution to improve encoder allocations.
// if this will stay it must be changed to one of the:
// - use buffer with allocations that can be adjusted using stats
// - use multiple buffers that increase in size (e.g. 16, 32, 64, 128 bytes).
var encoderPool = sync.Pool{
	New: func() interface{} {
		b := new(bytes.Buffer)
		b.Grow(64)
		return b
	},
}

func getEncoderBuffer() *bytes.Buffer {
	return encoderPool.Get().(*bytes.Buffer)
}

func putEncoderBuffer(b *bytes.Buffer) {
	b.Reset()
	encoderPool.Put(b)
}

// Encode value to a byte buffer.
func Encode(value Encodable) ([]byte, error) {
	b := getEncoderBuffer()
	defer putEncoderBuffer(b)
	_, err := EncodeTo(b, value)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, len(b.Bytes()))
	copy(buf, b.Bytes())
	return buf, nil
}

// Decode value from a byte buffer.
func Decode(buf []byte, value Decodable) error {
	if _, err := DecodeFrom(bytes.NewBuffer(buf), value); err != nil {
		return fmt.Errorf("decode from buffer: %w", err)
	}

	return nil
}
