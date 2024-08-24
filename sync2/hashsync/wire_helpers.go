package hashsync

import (
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type CompactHash32 struct {
	H *types.Hash32
}

// DecodeScale implements scale.Decodable.
func (c *CompactHash32) DecodeScale(dec *scale.Decoder) (int, error) {
	var h types.Hash32
	b, total, err := scale.DecodeByte(dec)
	switch {
	case err != nil:
		return total, err
	case b == 255:
		c.H = nil
		return total, nil
	case b != 0:
		n, err := scale.DecodeByteArray(dec, h[:b])
		total += n
		if err != nil {
			return total, err
		}
	}
	c.H = &h
	return total, nil
}

// EncodeScale implements scale.Encodable.
func (c *CompactHash32) EncodeScale(enc *scale.Encoder) (int, error) {
	if c.H == nil {
		return scale.EncodeByte(enc, 255)
	}

	b := byte(31)
	for b = 32; b > 0; b-- {
		if c.H[b-1] != 0 {
			break
		}
	}

	total, err := scale.EncodeByte(enc, b)
	if b == 0 || err != nil {
		return total, err
	}

	n, err := scale.EncodeByteArray(enc, c.H[:b])
	total += n
	return total, err
}

func (c *CompactHash32) ToOrdered() Ordered {
	if c.H == nil {
		return nil
	}
	return *c.H
}

func Hash32ToCompact(h types.Hash32) CompactHash32 {
	return CompactHash32{H: &h}
}

func OrderedToCompactHash32(h Ordered) CompactHash32 {
	hash := h.(types.Hash32)
	return CompactHash32{H: &hash}
}
