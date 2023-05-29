// Code generated by github.com/spacemeshos/go-scale/scalegen. DO NOT EDIT.

// nolint
package tortoise

import (
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
)

func (t *ConfigTrace) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Hdist))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Zdist))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.WindowSize))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.MaxExceptions))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.BadBeaconVoteDelayLayers))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.LayerSize))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.EpochSize))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *ConfigTrace) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Hdist = uint32(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Zdist = uint32(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.WindowSize = uint32(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.MaxExceptions = uint32(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.BadBeaconVoteDelayLayers = uint32(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.LayerSize = uint32(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.EpochSize = uint32(field)
	}
	return total, nil
}

func (t *AtxTrace) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeOption(enc, t.Header)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *AtxTrace) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeOption[types.ActivationTxHeader](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Header = field
	}
	return total, nil
}

func (t *WeakCoinTrace) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Layer))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeBool(enc, t.Coin)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *WeakCoinTrace) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Layer = types.LayerID(field)
	}
	{
		field, n, err := scale.DecodeBool(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Coin = field
	}
	return total, nil
}

func (t *BeaconTrace) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Epoch))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.Beacon[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *BeaconTrace) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Epoch = types.EpochID(field)
	}
	{
		n, err := scale.DecodeByteArray(dec, t.Beacon[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *BallotTrace) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeOption(enc, t.Ballot)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeBool(enc, t.Malicious)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *BallotTrace) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeOption[types.Ballot](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Ballot = field
	}
	{
		field, n, err := scale.DecodeBool(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Malicious = field
	}
	return total, nil
}

func (t *DecodeBallotTrace) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeOption(enc, t.Ballot)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStringWithLimit(enc, string(t.Error), 100000)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *DecodeBallotTrace) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeOption[types.Ballot](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Ballot = field
	}
	{
		field, n, err := scale.DecodeStringWithLimit(dec, 100000)
		if err != nil {
			return total, err
		}
		total += n
		t.Error = string(field)
	}
	return total, nil
}

func (t *StoreBallotTrace) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeByteArray(enc, t.ID[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeBool(enc, t.Malicious)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *StoreBallotTrace) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := scale.DecodeByteArray(dec, t.ID[:])
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
		t.Malicious = field
	}
	return total, nil
}

func (t *EncodeVotesTrace) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Layer))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeOption(enc, t.Opinion)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStringWithLimit(enc, string(t.Error), 100000)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *EncodeVotesTrace) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Layer = types.LayerID(field)
	}
	{
		field, n, err := scale.DecodeOption[types.Opinion](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Opinion = field
	}
	{
		field, n, err := scale.DecodeStringWithLimit(dec, 100000)
		if err != nil {
			return total, err
		}
		total += n
		t.Error = string(field)
	}
	return total, nil
}

func (t *TallyTrace) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Layer))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *TallyTrace) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Layer = types.LayerID(field)
	}
	return total, nil
}

func (t *HareTrace) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Layer))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.Vote[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *HareTrace) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Layer = types.LayerID(field)
	}
	{
		n, err := scale.DecodeByteArray(dec, t.Vote[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *ResultsTrace) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.From))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.To))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStringWithLimit(enc, string(t.Error), 100000)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSliceWithLimit(enc, t.Results, 100000)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *ResultsTrace) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.From = types.LayerID(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.To = types.LayerID(field)
	}
	{
		field, n, err := scale.DecodeStringWithLimit(dec, 100000)
		if err != nil {
			return total, err
		}
		total += n
		t.Error = string(field)
	}
	{
		field, n, err := scale.DecodeStructSliceWithLimit[result.Layer](dec, 100000)
		if err != nil {
			return total, err
		}
		total += n
		t.Results = field
	}
	return total, nil
}

func (t *UpdatesTrace) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := t.ResultsTrace.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *UpdatesTrace) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := t.ResultsTrace.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *BlockTrace) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := t.Header.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeBool(enc, t.Valid)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *BlockTrace) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := t.Header.DecodeScale(dec)
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
		t.Valid = field
	}
	return total, nil
}
