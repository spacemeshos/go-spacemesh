// Code generated by github.com/spacemeshos/go-scale/scalegen. DO NOT EDIT.

// nolint
package hare4

import (
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func (t *IterRound) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact8(enc, uint8(t.Iter))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact8(enc, uint8(t.Round))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *IterRound) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact8(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Iter = uint8(field)
	}
	{
		field, n, err := scale.DecodeCompact8(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Round = Round(field)
	}
	return total, nil
}

func (t *Value) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.Proposals, 2350)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeOption(enc, t.Reference)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.CompactProposals, 2350)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *Value) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeStructSliceWithLimit[types.ProposalID](dec, 2350)
		if err != nil {
			return total, err
		}
		total += n
		t.Proposals = field
	}
	{
		field, n, err := scale.DecodeOption[types.Hash32](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Reference = field
	}
	{
		field, n, err := scale.DecodeStructSliceWithLimit[types.CompactProposalID](dec, 2350)
		if err != nil {
			return total, err
		}
		total += n
		t.CompactProposals = field
	}
	return total, nil
}

func (t *Body) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Layer))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := t.IterRound.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := t.Value.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := t.Eligibility.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *Body) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Layer = types.LayerID(field)
	}
	{
		n, err := t.IterRound.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := t.Value.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := t.Eligibility.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *Message) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := t.Body.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.Sender[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.Signature[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *Message) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := t.Body.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.DecodeByteArray(dec, t.Sender[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.DecodeByteArray(dec, t.Signature[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *CompactIdRequest) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeByteArray(enc, t.MsgId[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *CompactIdRequest) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := scale.DecodeByteArray(dec, t.MsgId[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *CompactIdResponse) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.Ids, 2050)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *CompactIdResponse) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeStructSliceWithLimit[types.ProposalID](dec, 2050)
		if err != nil {
			return total, err
		}
		total += n
		t.Ids = field
	}
	return total, nil
}
