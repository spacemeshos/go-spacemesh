// Code generated by github.com/spacemeshos/go-scale/scalegen. DO NOT EDIT.

// nolint
package types

import (
	"github.com/spacemeshos/go-scale"
)

func (t *Proposal) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := t.InnerProposal.EncodeScale(enc)
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
	return total, nil
}

func (t *Proposal) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := t.InnerProposal.DecodeScale(dec)
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
	return total, nil
}

func (t *InnerProposal) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := t.Ballot.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSlice(enc, t.TxIDs)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.MeshHash[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *InnerProposal) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := t.Ballot.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeStructSlice[TransactionID](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.TxIDs = field
	}
	{
		n, err := scale.DecodeByteArray(dec, t.MeshHash[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}
