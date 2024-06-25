// Code generated by github.com/spacemeshos/go-scale/scalegen. DO NOT EDIT.

// nolint
package wire

import (
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func (t *ActivationTxV2) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.PublishEpoch))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.PositioningATX[:])
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
		n, err := scale.EncodeOption(enc, t.Initial)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.PreviousATXs, 256)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.NiPosts, 2)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact64(enc, uint64(t.VRFNonce))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.Marriages, 256)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeOption(enc, t.MarriageATX)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.SmesherID[:])
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

func (t *ActivationTxV2) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.PublishEpoch = types.EpochID(field)
	}
	{
		n, err := scale.DecodeByteArray(dec, t.PositioningATX[:])
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
		field, n, err := scale.DecodeOption[InitialAtxPartsV2](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Initial = field
	}
	{
		field, n, err := scale.DecodeStructSliceWithLimit[types.ATXID](dec, 256)
		if err != nil {
			return total, err
		}
		total += n
		t.PreviousATXs = field
	}
	{
		field, n, err := scale.DecodeStructSliceWithLimit[NiPostsV2](dec, 2)
		if err != nil {
			return total, err
		}
		total += n
		t.NiPosts = field
	}
	{
		field, n, err := scale.DecodeCompact64(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.VRFNonce = uint64(field)
	}
	{
		field, n, err := scale.DecodeStructSliceWithLimit[MarriageCertificate](dec, 256)
		if err != nil {
			return total, err
		}
		total += n
		t.Marriages = field
	}
	{
		field, n, err := scale.DecodeOption[types.ATXID](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.MarriageATX = field
	}
	{
		n, err := scale.DecodeByteArray(dec, t.SmesherID[:])
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

func (t *InitialAtxPartsV2) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeByteArray(enc, t.CommitmentATX[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := t.Post.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *InitialAtxPartsV2) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := scale.DecodeByteArray(dec, t.CommitmentATX[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := t.Post.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *MarriageCertificate) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeByteArray(enc, t.ReferenceAtx[:])
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

func (t *MarriageCertificate) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := scale.DecodeByteArray(dec, t.ReferenceAtx[:])
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

func (t *MerkleProofV2) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.Nodes, 32)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeUint64SliceWithLimit(enc, t.LeafIndices, 256)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *MerkleProofV2) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeStructSliceWithLimit[types.Hash32](dec, 32)
		if err != nil {
			return total, err
		}
		total += n
		t.Nodes = field
	}
	{
		field, n, err := scale.DecodeUint64SliceWithLimit(dec, 256)
		if err != nil {
			return total, err
		}
		total += n
		t.LeafIndices = field
	}
	return total, nil
}

func (t *SubPostV2) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.MarriageIndex))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.PrevATXIndex))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := t.Post.EncodeScale(enc)
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
	return total, nil
}

func (t *SubPostV2) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.MarriageIndex = uint32(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.PrevATXIndex = uint32(field)
	}
	{
		n, err := t.Post.DecodeScale(dec)
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
	return total, nil
}

func (t *NiPostsV2) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := t.Membership.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.Challenge[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.Posts, 256)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *NiPostsV2) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := t.Membership.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.DecodeByteArray(dec, t.Challenge[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeStructSliceWithLimit[SubPostV2](dec, 256)
		if err != nil {
			return total, err
		}
		total += n
		t.Posts = field
	}
	return total, nil
}
