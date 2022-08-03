// Code generated by github.com/spacemeshos/go-scale/scalegen. DO NOT EDIT.

package types

import (
	"github.com/spacemeshos/go-scale"
)

func (t *Block) EncodeScale(enc *scale.Encoder) (total int, err error) {
	if n, err := t.InnerBlock.EncodeScale(enc); err != nil {
		return total, err
	} else {
		total += n
	}
	return total, nil
}

func (t *Block) DecodeScale(dec *scale.Decoder) (total int, err error) {
	if n, err := t.InnerBlock.DecodeScale(dec); err != nil {
		return total, err
	} else {
		total += n
	}
	return total, nil
}

func (t *InnerBlock) EncodeScale(enc *scale.Encoder) (total int, err error) {
	if n, err := t.LayerIndex.EncodeScale(enc); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.EncodeCompact64(enc, t.TickHeight); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.EncodeStructSlice(enc, t.Rewards); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.EncodeStructSlice(enc, t.TxIDs); err != nil {
		return total, err
	} else {
		total += n
	}
	return total, nil
}

func (t *InnerBlock) DecodeScale(dec *scale.Decoder) (total int, err error) {
	if n, err := t.LayerIndex.DecodeScale(dec); err != nil {
		return total, err
	} else {
		total += n
	}
	if field, n, err := scale.DecodeCompact64(dec); err != nil {
		return total, err
	} else {
		total += n
		t.TickHeight = field
	}
	if field, n, err := scale.DecodeStructSlice[AnyReward](dec); err != nil {
		return total, err
	} else {
		total += n
		t.Rewards = field
	}
	if field, n, err := scale.DecodeStructSlice[TransactionID](dec); err != nil {
		return total, err
	} else {
		total += n
		t.TxIDs = field
	}
	return total, nil
}

func (t *RatNum) EncodeScale(enc *scale.Encoder) (total int, err error) {
	if n, err := scale.EncodeCompact64(enc, t.Num); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.EncodeCompact64(enc, t.Denom); err != nil {
		return total, err
	} else {
		total += n
	}
	return total, nil
}

func (t *RatNum) DecodeScale(dec *scale.Decoder) (total int, err error) {
	if field, n, err := scale.DecodeCompact64(dec); err != nil {
		return total, err
	} else {
		total += n
		t.Num = field
	}
	if field, n, err := scale.DecodeCompact64(dec); err != nil {
		return total, err
	} else {
		total += n
		t.Denom = field
	}
	return total, nil
}

func (t *AnyReward) EncodeScale(enc *scale.Encoder) (total int, err error) {
	if n, err := scale.EncodeByteArray(enc, t.Coinbase[:]); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := t.Weight.EncodeScale(enc); err != nil {
		return total, err
	} else {
		total += n
	}
	return total, nil
}

func (t *AnyReward) DecodeScale(dec *scale.Decoder) (total int, err error) {
	if n, err := scale.DecodeByteArray(dec, t.Coinbase[:]); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := t.Weight.DecodeScale(dec); err != nil {
		return total, err
	} else {
		total += n
	}
	return total, nil
}

func (t *BlockContextualValidity) EncodeScale(enc *scale.Encoder) (total int, err error) {
	if n, err := scale.EncodeByteArray(enc, t.ID[:]); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.EncodeBool(enc, t.Validity); err != nil {
		return total, err
	} else {
		total += n
	}
	return total, nil
}

func (t *BlockContextualValidity) DecodeScale(dec *scale.Decoder) (total int, err error) {
	if n, err := scale.DecodeByteArray(dec, t.ID[:]); err != nil {
		return total, err
	} else {
		total += n
	}
	if field, n, err := scale.DecodeBool(dec); err != nil {
		return total, err
	} else {
		total += n
		t.Validity = field
	}
	return total, nil
}
