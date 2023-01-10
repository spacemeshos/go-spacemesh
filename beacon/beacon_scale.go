// Code generated by github.com/spacemeshos/go-scale/scalegen. DO NOT EDIT.

// nolint
package beacon

import (
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func (t *ProposalVrfMessage) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact16(enc, uint16(t.Type))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Epoch))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *ProposalVrfMessage) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact16(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Type = types.EligibilityType(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Epoch = uint32(field)
	}
	return total, nil
}
