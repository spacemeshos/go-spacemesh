// Code generated by github.com/spacemeshos/go-scale/scalegen. DO NOT EDIT.

// nolint
package types

import (
	"github.com/spacemeshos/go-scale"
)

func (t *ActivationTxHeader) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := t.NIPostChallenge.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.Coinbase[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.NumUnits))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.EffectiveNumUnits))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeOption(enc, t.VRFNonce)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.ID[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.NodeID[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact64(enc, uint64(t.BaseTickHeight))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact64(enc, uint64(t.TickCount))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact64(enc, uint64(t.Received))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeBool(enc, t.Golden)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *ActivationTxHeader) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := t.NIPostChallenge.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.DecodeByteArray(dec, t.Coinbase[:])
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
		t.NumUnits = uint32(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.EffectiveNumUnits = uint32(field)
	}
	{
		field, n, err := scale.DecodeOption[VRFPostIndex](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.VRFNonce = field
	}
	{
		n, err := scale.DecodeByteArray(dec, t.ID[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.DecodeByteArray(dec, t.NodeID[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeCompact64(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.BaseTickHeight = uint64(field)
	}
	{
		field, n, err := scale.DecodeCompact64(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.TickCount = uint64(field)
	}
	{
		field, n, err := scale.DecodeCompact64(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Received = uint64(field)
	}
	{
		field, n, err := scale.DecodeBool(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Golden = field
	}
	return total, nil
}
