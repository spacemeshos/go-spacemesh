package codec

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	"github.com/spacemeshos/go-scale"
)

// Encodable is an interface that must be implemented by a struct to be encoded.
type Encodable = scale.Encodable

// Decodable is an interface that must be implemented bya struct to be decoded.
type Decodable = scale.Decodable

// EncodeTo encodes value to a writer stream.
func EncodeTo(w io.Writer, value Encodable) (int, error) {
	return value.EncodeScale(scale.NewEncoder(w))
}

// DecodeFrom decodes a value using data from a reader stream.
func DecodeFrom(r io.Reader, value Decodable) (int, error) {
	return value.DecodeScale(scale.NewDecoder(r))
}

// TODO(dshulyak) this is a temporary solution to improve encoder allocations.
// if this will stay it must be changed to one of the:
// - use buffer with allocations that can be adjusted using stats
// - use multiple buffers that increase in size (e.g. 16, 32, 64, 128 bytes).
var encoderPool = sync.Pool{
	New: func() any {
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
	n, err := DecodeFrom(bytes.NewBuffer(buf), value)
	if err != nil {
		return fmt.Errorf("decode from buffer: %w", err)
	}
	if n != len(buf) {
		return fmt.Errorf("decode from buffer: not all bytes were consumed")
	}
	return nil
}

// DecodeSome value from a byte buffer without checking for full consumption.
func DecodeSome(buf []byte, value Decodable) error {
	if _, err := DecodeFrom(bytes.NewBuffer(buf), value); err != nil {
		return fmt.Errorf("decode from buffer: %w", err)
	}

	return nil
}

// EncodeSlice encodes slice to a buffer.
func EncodeSlice[V any, H scale.EncodablePtr[V]](value []V) ([]byte, error) {
	var b bytes.Buffer
	_, err := scale.EncodeStructSlice[V, H](scale.NewEncoder(&b), value)
	if err != nil {
		return nil, fmt.Errorf("encode struct slice: %w", err)
	}
	return b.Bytes(), nil
}

// DecodeSlice decodes slice from a buffer.
func DecodeSlice[V any, H scale.DecodablePtr[V]](buf []byte) ([]V, error) {
	v, _, err := scale.DecodeStructSlice[V, H](scale.NewDecoder(bytes.NewReader(buf)))
	if err != nil {
		return nil, fmt.Errorf("decode struct slice: %w", err)
	}
	return v, nil
}

// EncodeCompact16 encodes uint16 to a buffer.
func EncodeCompact16(w io.Writer, value uint16) (int, error) {
	return scale.EncodeCompact16(scale.NewEncoder(w), value)
}

// DecodeCompact16 decodes uint16 from a buffer.
func DecodeCompact16(w io.Reader) (uint16, int, error) {
	return scale.DecodeCompact16(scale.NewDecoder(w))
}

// EncodeStringSlice encodes []string to a buffer.
func EncodeStringSlice(w io.Writer, value []string) (int, error) {
	return scale.EncodeStringSlice(scale.NewEncoder(w), value)
}

// DecodeStringSlice decodes []string from a buffer.
func DecodeStringSlice(w io.Reader) ([]string, int, error) {
	return scale.DecodeStringSlice(scale.NewDecoder(w))
}

// EncodeByteSlice encodes []string to a buffer.
func EncodeByteSlice(w io.Writer, value []byte) (int, error) {
	return scale.EncodeByteSlice(scale.NewEncoder(w), value)
}

// DecodeByteSlice decodes []string from a buffer.
func DecodeByteSlice(w io.Reader) ([]byte, int, error) {
	return scale.DecodeByteSlice(scale.NewDecoder(w))
}
