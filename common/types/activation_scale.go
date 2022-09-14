// Code generated by github.com/spacemeshos/go-scale/scalegen. DO NOT EDIT.

// nolint
package types

import (
	"github.com/spacemeshos/go-scale"
)

func (t *NIPostChallenge) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact64(enc, uint64(t.Sequence))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.PrevATXID[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := t.PubLayerID.EncodeScale(enc)
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
		n, err := scale.EncodeByteSlice(enc, t.InitialPostIndices)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *NIPostChallenge) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact64(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Sequence = uint64(field)
	}
	{
		n, err := scale.DecodeByteArray(dec, t.PrevATXID[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := t.PubLayerID.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.DecodeByteArray(dec, t.PositioningATX[:])
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
		t.InitialPostIndices = field
	}
	return total, nil
}

func (t *InnerActivationTx) EncodeScale(enc *scale.Encoder) (total int, err error) {
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
		n, err := scale.EncodeOption(enc, t.NIPost)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeOption(enc, t.InitialPost)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *InnerActivationTx) DecodeScale(dec *scale.Decoder) (total int, err error) {
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
		field, n, err := scale.DecodeOption[NIPost](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.NIPost = field
	}
	{
		field, n, err := scale.DecodeOption[Post](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.InitialPost = field
	}
	return total, nil
}

func (t *ActivationTx) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := t.InnerActivationTx.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteSlice(enc, t.Sig)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *ActivationTx) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := t.InnerActivationTx.DecodeScale(dec)
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
		t.Sig = field
	}
	return total, nil
}

func (t *PoetProof) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := t.MerkleProof.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeSliceOfByteSlice(enc, t.Members)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact64(enc, uint64(t.LeafCount))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *PoetProof) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := t.MerkleProof.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeSliceOfByteSlice(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Members = field
	}
	{
		field, n, err := scale.DecodeCompact64(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.LeafCount = uint64(field)
	}
	return total, nil
}

func (t *PoetProofMessage) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := t.PoetProof.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteSlice(enc, t.PoetServiceID)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeString(enc, string(t.RoundID))
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

func (t *PoetProofMessage) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := t.PoetProof.DecodeScale(dec)
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
		t.PoetServiceID = field
	}
	{
		field, n, err := scale.DecodeString(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.RoundID = string(field)
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

func (t *PoetRound) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeString(enc, string(t.ID))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *PoetRound) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeString(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.ID = string(field)
	}
	return total, nil
}

func (t *NIPost) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeOption(enc, t.Challenge)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeOption(enc, t.Post)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeOption(enc, t.PostMetadata)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *NIPost) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeOption[Hash32](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Challenge = field
	}
	{
		field, n, err := scale.DecodeOption[Post](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Post = field
	}
	{
		field, n, err := scale.DecodeOption[PostMetadata](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.PostMetadata = field
	}
	return total, nil
}

func (t *PostMetadata) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeByteSlice(enc, t.Challenge)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact8(enc, uint8(t.BitsPerLabel))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact64(enc, uint64(t.LabelsPerUnit))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.K1))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.K2))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *PostMetadata) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeByteSlice(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Challenge = field
	}
	{
		field, n, err := scale.DecodeCompact8(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.BitsPerLabel = uint8(field)
	}
	{
		field, n, err := scale.DecodeCompact64(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.LabelsPerUnit = uint64(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.K1 = uint32(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.K2 = uint32(field)
	}
	return total, nil
}
