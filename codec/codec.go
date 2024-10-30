package codec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/spacemeshos/go-scale"
)

var ErrShortRead = errors.New("decode from buffer: not all bytes were consumed")

// Encodable is an interface that must be implemented by a struct to be encoded.
type Encodable = scale.Encodable

// Decodable is an interface that must be implemented bya struct to be decoded.
type Decodable = scale.Decodable

// EncodeTo encodes value to a writer stream.
func EncodeTo(w io.Writer, value Encodable) (int, error) {
	return value.EncodeScale(scale.NewEncoder(w, scale.WithEncodeMaxNested(6)))
}

// DecodeFrom decodes a value using data from a reader stream.
func DecodeFrom(r io.Reader, value Decodable) (int, error) {
	return value.DecodeScale(scale.NewDecoder(r, scale.WithDecodeMaxNested(6)))
}

// TODO(dshulyak) this is a temporary solution to improve encoder allocations.
// If this will stay it must be changed to one of the:
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

func MustEncodeTo(w io.Writer, value Encodable) {
	_, err := EncodeTo(w, value)
	if err != nil {
		panic(err)
	}
}

func MustEncode(value Encodable) []byte {
	buf, err := Encode(value)
	if err != nil {
		panic(err)
	}
	return buf
}

// Encode value to a byte buffer.
func Encode(value Encodable) ([]byte, error) {
	b := getEncoderBuffer()
	defer putEncoderBuffer(b)
	n, err := EncodeTo(b, value)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, n)
	copy(buf, b.Bytes())
	return buf, nil
}

func MustDecode(buf []byte, value Decodable) {
	err := Decode(buf, value)
	if err != nil {
		panic(err)
	}
}

// Decode value from a byte buffer.
func Decode(buf []byte, value Decodable) error {
	n, err := DecodeFrom(bytes.NewBuffer(buf), value)
	if err != nil {
		return fmt.Errorf("decode from buffer: %w", err)
	}
	if n != len(buf) {
		return ErrShortRead
	}
	return nil
}

// EncodeSlice encodes slice with a length prefix.
func EncodeSlice[V any, H scale.EncodablePtr[V]](value []V) ([]byte, error) {
	var b bytes.Buffer
	_, err := scale.EncodeStructSlice[V, H](scale.NewEncoder(&b), value)
	if err != nil {
		return nil, fmt.Errorf("encode struct slice: %w", err)
	}
	return b.Bytes(), nil
}

// MustEncodeSlice encodes slice with a length prefix or panics on error.
func MustEncodeSlice[V any, H scale.EncodablePtr[V]](value []V) []byte {
	buf, err := EncodeSlice[V, H](value)
	if err != nil {
		panic(err)
	}
	return buf
}

// DecodeSliceFromReader accepts a reader and decodes slice with a length prefix.
func DecodeSliceFromReader[V any, H scale.DecodablePtr[V]](r io.Reader) ([]V, error) {
	v, _, err := scale.DecodeStructSlice[V, H](scale.NewDecoder(r))
	if err != nil {
		return nil, fmt.Errorf("decode struct slice: %w", err)
	}
	return v, nil
}

// MustDecodeSliceFromReader decodes slice with a length prefix or panics on error.
func MustDecodeSliceFromReader[V any, H scale.DecodablePtr[V]](r io.Reader) []V {
	v, err := DecodeSliceFromReader[V, H](r)
	if err != nil {
		panic(err)
	}
	return v
}

// DecodeSlice decodes slice from a buffer.
func DecodeSlice[V any, H scale.DecodablePtr[V]](buf []byte) ([]V, error) {
	return DecodeSliceFromReader[V, H](bytes.NewReader(buf))
}

// EncodeCompact16 encodes uint16 to a buffer.
// ReadSlice decodes slice from am io.Reader.
func ReadSlice[V any, H scale.DecodablePtr[V]](r io.Reader) ([]V, int, error) {
	v, n, err := scale.DecodeStructSlice[V, H](scale.NewDecoder(r))
	if err != nil {
		return nil, 0, fmt.Errorf("read struct slice: %w", err)
	}
	return v, n, nil
}

// EncodeCompact16 encodes uint16 to an io.Writer.
func EncodeCompact16(w io.Writer, value uint16) (int, error) {
	return scale.EncodeCompact16(scale.NewEncoder(w), value)
}

// DecodeCompact16 decodes uint16 from an io.Reader.
func DecodeCompact16(r io.Reader) (uint16, int, error) {
	return scale.DecodeCompact16(scale.NewDecoder(r))
}

// EncodeStringSlice encodes []string to an io.Writer.
func EncodeStringSlice(w io.Writer, value []string) (int, error) {
	return scale.EncodeStringSlice(scale.NewEncoder(w), value)
}

// DecodeStringSlice decodes []string from an io.Reader.
func DecodeStringSlice(r io.Reader) ([]string, int, error) {
	return scale.DecodeStringSlice(scale.NewDecoder(r))
}

// EncodeByteSlice encodes []string to an io.Writer.
func EncodeByteSlice(w io.Writer, value []byte) (int, error) {
	return scale.EncodeByteSlice(scale.NewEncoder(w), value)
}

// DecodeByteSlice decodes []string from an io.Reader.
func DecodeByteSlice(r io.Reader) ([]byte, int, error) {
	return scale.DecodeByteSlice(scale.NewDecoder(r))
}

// EncodeLen encodes a length value to an io.Writer.
func EncodeLen(w io.Writer, value uint32) (int, error) {
	return scale.EncodeCompact32(scale.NewEncoder(w), value)
}

// DecodeLen decodes a length value from an io.Reader.
func DecodeLen(r io.Reader) (uint32, int, error) {
	return scale.DecodeCompact32(scale.NewDecoder(r))
}

// DecodeStringWithLimit decodes a string from an io.Reader, limiting the maximum length.
func DecodeStringWithLimit(r io.Reader, limit uint32) (string, int, error) {
	return scale.DecodeStringWithLimit(scale.NewDecoder(r), limit)
}
