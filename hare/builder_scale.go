// Code generated by github.com/spacemeshos/go-scale/scalegen. DO NOT EDIT.

// nolint
package hare

import (
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func (t *Metadata) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := t.Layer.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Round))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.MsgHash[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *Metadata) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := t.Layer.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Round = uint32(field)
	}
	{
		n, err := scale.DecodeByteArray(dec, t.MsgHash[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *Message) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := t.Metadata.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteSlice(enc, t.Signature)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeOption(enc, t.InnerMsg)
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

func (t *Message) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := t.Metadata.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeByteSlice(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Signature = field
	}
	{
		field, n, err := scale.DecodeOption[InnerMessage](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.InnerMsg = field
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

func (t *Certificate) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeStructSlice(enc, t.Values)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeOption(enc, t.AggMsgs)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *Certificate) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeStructSlice[types.ProposalID](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Values = field
	}
	{
		field, n, err := scale.DecodeOption[AggregatedMessages](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.AggMsgs = field
	}
	return total, nil
}

func (t *AggregatedMessages) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeStructSlice(enc, t.Messages)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *AggregatedMessages) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeStructSlice[Message](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Messages = field
	}
	return total, nil
}

func (t *InnerMessage) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact8(enc, uint8(t.Type))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.CommittedRound))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSlice(enc, t.Values)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeOption(enc, t.Svp)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeOption(enc, t.Cert)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *InnerMessage) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact8(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Type = MessageType(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.CommittedRound = uint32(field)
	}
	{
		field, n, err := scale.DecodeStructSlice[types.ProposalID](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Values = field
	}
	{
		field, n, err := scale.DecodeOption[AggregatedMessages](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Svp = field
	}
	{
		field, n, err := scale.DecodeOption[Certificate](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Cert = field
	}
	return total, nil
}
