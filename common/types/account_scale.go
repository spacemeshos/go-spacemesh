// Code generated by github.com/spacemeshos/go-scale/scalegen. DO NOT EDIT.

// nolint
package types

import (
	"github.com/spacemeshos/go-scale"
)

func (t *Account) EncodeScale(enc *scale.Encoder) (total int, err error) {
	
	{
		n, err := scale.EncodeByteArray(enc, t.Address[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeBool(enc, t.Initialized)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact64(enc, uint64(t.NextNonce))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact64(enc, uint64(t.Balance))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeOption(enc, t.TemplateAddress)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteSliceWithLimit(enc, t.State, 10000)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *Account) DecodeScale(dec *scale.Decoder) (total int, err error) {
	
	{
		n, err := scale.DecodeByteArray(dec, t.Address[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeBool(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Initialized = field
	}
	{
		field, n, err := scale.DecodeCompact64(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.NextNonce = uint64(field)
	}
	{
		field, n, err := scale.DecodeCompact64(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Balance = uint64(field)
	}
	{
		field, n, err := scale.DecodeOption[Address](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.TemplateAddress = field
	}
	{
		field, n, err := scale.DecodeByteSliceWithLimit(dec, 10000)
		if err != nil {
			return total, err
		}
		total += n
		t.State = field
	}
	return total, nil
}
