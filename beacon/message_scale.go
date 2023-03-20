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
		n, err := scale.EncodeCompact64(enc, uint64(t.Nonce))
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
		field, n, err := scale.DecodeCompact64(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Nonce = types.VRFPostIndex(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Epoch = types.EpochID(field)
	}
	return total, nil
}

func (t *ProposalMessage) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.EpochID))
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
		n, err := scale.EncodeByteSliceWithLimit(enc, t.VRFSignature, 64)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *ProposalMessage) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.EpochID = types.EpochID(field)
	}
	{
		n, err := scale.DecodeByteArray(dec, t.NodeID[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeByteSliceWithLimit(dec, 64)
		if err != nil {
			return total, err
		}
		total += n
		t.VRFSignature = field
	}
	return total, nil
}

func (t *Proposal) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeByteSliceWithLimit(enc, t.Value, 32)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *Proposal) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeByteSliceWithLimit(dec, 32)
		if err != nil {
			return total, err
		}
		total += n
		t.Value = field
	}
	return total, nil
}

func (t *FirstVotingMessageBody) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.EpochID))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.ValidProposals, 32)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.PotentiallyValidProposals, 32)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *FirstVotingMessageBody) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.EpochID = types.EpochID(field)
	}
	{
		field, n, err := scale.DecodeStructSliceWithLimit[Proposal](dec, 32)
		if err != nil {
			return total, err
		}
		total += n
		t.ValidProposals = field
	}
	{
		field, n, err := scale.DecodeStructSliceWithLimit[Proposal](dec, 32)
		if err != nil {
			return total, err
		}
		total += n
		t.PotentiallyValidProposals = field
	}
	return total, nil
}

func (t *FirstVotingMessage) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := t.FirstVotingMessageBody.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteSliceWithLimit(enc, t.Signature, 64)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *FirstVotingMessage) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := t.FirstVotingMessageBody.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeByteSliceWithLimit(dec, 64)
		if err != nil {
			return total, err
		}
		total += n
		t.Signature = field
	}
	return total, nil
}

func (t *FollowingVotingMessageBody) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.EpochID))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.RoundID))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteSliceWithLimit(enc, t.VotesBitVector, 32)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *FollowingVotingMessageBody) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.EpochID = types.EpochID(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.RoundID = types.RoundID(field)
	}
	{
		field, n, err := scale.DecodeByteSliceWithLimit(dec, 32)
		if err != nil {
			return total, err
		}
		total += n
		t.VotesBitVector = field
	}
	return total, nil
}

func (t *FollowingVotingMessage) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := t.FollowingVotingMessageBody.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteSliceWithLimit(enc, t.Signature, 64)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *FollowingVotingMessage) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := t.FollowingVotingMessageBody.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeByteSliceWithLimit(dec, 64)
		if err != nil {
			return total, err
		}
		total += n
		t.Signature = field
	}
	return total, nil
}
