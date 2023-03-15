// Code generated by github.com/spacemeshos/go-scale/scalegen. DO NOT EDIT.

// nolint
package fetch

import (
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
)

func (t *RequestMessage) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeStringWithLimit(enc, string(t.Hint), 256)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.Hash[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *RequestMessage) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeStringWithLimit(dec, 256)
		if err != nil {
			return total, err
		}
		total += n
		t.Hint = datastore.Hint(field)
	}
	{
		n, err := scale.DecodeByteArray(dec, t.Hash[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *ResponseMessage) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeByteArray(enc, t.Hash[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteSliceWithLimit(enc, t.Data, 1024)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *ResponseMessage) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := scale.DecodeByteArray(dec, t.Hash[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeByteSliceWithLimit(dec, 1024)
		if err != nil {
			return total, err
		}
		total += n
		t.Data = field
	}
	return total, nil
}

func (t *RequestBatch) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeByteArray(enc, t.ID[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.Requests, 32)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *RequestBatch) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := scale.DecodeByteArray(dec, t.ID[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeStructSliceWithLimit[RequestMessage](dec, 32)
		if err != nil {
			return total, err
		}
		total += n
		t.Requests = field
	}
	return total, nil
}

func (t *ResponseBatch) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeByteArray(enc, t.ID[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.Responses, 32)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *ResponseBatch) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := scale.DecodeByteArray(dec, t.ID[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeStructSliceWithLimit[ResponseMessage](dec, 32)
		if err != nil {
			return total, err
		}
		total += n
		t.Responses = field
	}
	return total, nil
}

func (t *MeshHashRequest) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := t.From.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := t.To.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Delta))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Steps))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *MeshHashRequest) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := t.From.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := t.To.DecodeScale(dec)
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
		t.Delta = uint32(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Steps = uint32(field)
	}
	return total, nil
}

func (t *MeshHashes) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.Layers, 32)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.Hashes, 32)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *MeshHashes) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeStructSliceWithLimit[types.LayerID](dec, 32)
		if err != nil {
			return total, err
		}
		total += n
		t.Layers = field
	}
	{
		field, n, err := scale.DecodeStructSliceWithLimit[types.Hash32](dec, 32)
		if err != nil {
			return total, err
		}
		total += n
		t.Hashes = field
	}
	return total, nil
}

func (t *MaliciousIDs) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.NodeIDs, 32)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *MaliciousIDs) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeStructSliceWithLimit[types.NodeID](dec, 32)
		if err != nil {
			return total, err
		}
		total += n
		t.NodeIDs = field
	}
	return total, nil
}

func (t *EpochData) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.AtxIDs, 32)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *EpochData) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeStructSliceWithLimit[types.ATXID](dec, 32)
		if err != nil {
			return total, err
		}
		total += n
		t.AtxIDs = field
	}
	return total, nil
}

func (t *LayerData) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.Ballots, 32)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.Blocks, 32)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *LayerData) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeStructSliceWithLimit[types.BallotID](dec, 32)
		if err != nil {
			return total, err
		}
		total += n
		t.Ballots = field
	}
	{
		field, n, err := scale.DecodeStructSliceWithLimit[types.BlockID](dec, 32)
		if err != nil {
			return total, err
		}
		total += n
		t.Blocks = field
	}
	return total, nil
}

func (t *LayerOpinion) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeByteArray(enc, t.PrevAggHash[:])
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

func (t *LayerOpinion) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := scale.DecodeByteArray(dec, t.PrevAggHash[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeOption[types.Certificate](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Cert = field
	}
	return total, nil
}
