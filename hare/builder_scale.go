// Code generated by github.com/spacemeshos/go-scale/scalegen. DO NOT EDIT.

// nolint
package hare

import (
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func (t *Message) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeByteSlice(enc, t.Sig)
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
	return total, nil
}

func (t *Message) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeByteSlice(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Sig = field
	}
	{
		field, n, err := scale.DecodeOption[InnerMessage](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.InnerMsg = field
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
		n, err := t.InstanceID.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.K))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Ki))
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
		n, err := scale.EncodeByteSlice(enc, t.RoleProof)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact16(enc, uint16(t.EligibilityCount))
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
		n, err := t.InstanceID.DecodeScale(dec)
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
		t.K = uint32(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Ki = uint32(field)
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
		field, n, err := scale.DecodeByteSlice(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.RoleProof = field
	}
	{
		field, n, err := scale.DecodeCompact16(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.EligibilityCount = uint16(field)
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

func (t *MessageBuilder) EncodeScale(enc *scale.Encoder) (total int, err error) {
	return total, nil
}

func (t *MessageBuilder) DecodeScale(dec *scale.Decoder) (total int, err error) {
	return total, nil
}
