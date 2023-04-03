// Code generated by github.com/spacemeshos/go-scale/scalegen. DO NOT EDIT.

// nolint
package types

import (
	"github.com/spacemeshos/go-scale"
)

func (t *HareEligibilityGossip) EncodeScale(enc *scale.Encoder) (total int, err error) {
	
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Round))
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
		n, err := t.Eligibility.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *HareEligibilityGossip) DecodeScale(dec *scale.Decoder) (total int, err error) {
	
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Round = uint32(field)
	}
	{
		n, err := scale.DecodeByteArray(dec, t.NodeID[:])
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

func (t *HareEligibility) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeByteArray(enc, t.Proof[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact16(enc, uint16(t.Count))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *HareEligibility) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := scale.DecodeByteArray(dec, t.Proof[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeCompact16(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Count = uint16(field)
	}
	return total, nil
}

func (t *VotingEligibility) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.J))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.Sig[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *VotingEligibility) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.J = uint32(field)
	}
	{
		n, err := scale.DecodeByteArray(dec, t.Sig[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}
