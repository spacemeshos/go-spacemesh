package rangesync

import (
	"errors"
	"fmt"

	"github.com/spacemeshos/go-scale"
)

// CompactHash encodes hashes in a compact form, skipping trailing zeroes.
// It also supports a nil hash (no value).
// The encoding format is as follows:
// byte 0:     spec byte
// bytes 1..n: data bytes
//
// The format of the spec byte is as follows:
// bits 0..5:  number of non-zero leading bytes
// bits 6..7:  hash type
//
// The following hash types are supported:
// 0:    nil hash
// 1:    32-byte hash
// 2,3:  reserved

// NOTE: when adding new hash types, we need to add a mechanism that makes sure that every
// received hash is of the expected type. Alternatively, we need to add some kind of
// context to the scale.Decoder / scale.Encoder, which may contain the size of hashes to
// be used.

const (
	compactHashTypeNil  = 0
	compactHashType32   = 1
	compactHashSizeBits = 6
	maxCompactHashSize  = 32
)

var errInvalidCompactHash = errors.New("invalid compact hash")

type CompactHash struct {
	H KeyBytes
}

// DecodeScale implements scale.Decodable.
func (c *CompactHash) DecodeScale(dec *scale.Decoder) (int, error) {
	var h [maxCompactHashSize]byte
	b, total, err := scale.DecodeByte(dec)
	switch {
	case err != nil:
		return total, err
	case b>>compactHashSizeBits == compactHashTypeNil:
		c.H = nil
		return total, nil
	case b>>compactHashSizeBits != compactHashType32:
		return total, errInvalidCompactHash
	case b != 0:
		l := b & ((1 << compactHashSizeBits) - 1)
		n, err := scale.DecodeByteArray(dec, h[:l])
		total += n
		if err != nil {
			return total, err
		}
	}
	c.H = h[:]
	return total, nil
}

// EncodeScale implements scale.Encodable.
func (c *CompactHash) EncodeScale(enc *scale.Encoder) (int, error) {
	if c.H == nil {
		return scale.EncodeByte(enc, compactHashTypeNil<<compactHashSizeBits)
	}

	if len(c.H) != 32 {
		panic("BUG: only 32-byte hashes are supported at the moment")
	}

	var b byte
	for b = 32; b > 0; b-- {
		if c.H[b-1] != 0 {
			break
		}
	}

	total, err := scale.EncodeByte(enc, b|(compactHashType32<<compactHashSizeBits))
	if b == 0 || err != nil {
		return total, err
	}

	n, err := scale.EncodeByteArray(enc, c.H[:b])
	total += n
	return total, err
}

func (c *CompactHash) ToOrdered() KeyBytes {
	if c.H == nil {
		return nil
	}
	return c.H
}

func chash(h KeyBytes) CompactHash {
	if h == nil {
		return CompactHash{}
	}
	return CompactHash{H: h.Clone()}
}

const (
	keyCollectionLimit = 100000000
)

// KeyCollection represents a collection of keys of the same size.
type KeyCollection struct {
	Keys []KeyBytes
}

// DecodeScale implements scale.Decodable.
func (c *KeyCollection) DecodeScale(dec *scale.Decoder) (int, error) {
	size, total, err := scale.DecodeLen(dec, keyCollectionLimit)
	if err != nil {
		return total, err
	}
	if size == 0 {
		c.Keys = nil
		return total, nil
	}
	hashType, n, err := scale.DecodeByte(dec)
	total += n
	if err != nil {
		return total, err
	}
	if hashType != compactHashType32 {
		return total, errInvalidCompactHash
	}
	c.Keys = make([]KeyBytes, size)
	for p := range size {
		var h [maxCompactHashSize]byte
		n, err := scale.DecodeByteArray(dec, h[:])
		total += n
		if err != nil {
			return total, err
		}
		c.Keys[p] = h[:]
	}

	return total, nil
}

// EncodeScale implements scale.Encodable.
func (c *KeyCollection) EncodeScale(enc *scale.Encoder) (int, error) {
	total, err := scale.EncodeLen(enc, uint32(len(c.Keys)), keyCollectionLimit)
	if err != nil || len(c.Keys) == 0 {
		return total, err
	}
	n, err := scale.EncodeByte(enc, compactHashType32)
	total += n
	if err != nil {
		return total, err
	}
	for _, k := range c.Keys {
		if len(k) != 32 {
			return total, fmt.Errorf("bad key size %d", len(k))
		}
		n, err := scale.EncodeByteArray(enc, k)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
