// Code generated by github.com/spacemeshos/go-scale/scalegen. DO NOT EDIT.

package types

import (
	"github.com/spacemeshos/go-scale"
)

func (t *Transaction) EncodeScale(enc *scale.Encoder) (total int, err error) {
	if n, err := t.RawTx.EncodeScale(enc); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.EncodeOption(enc, t.TxHeader); err != nil {
		return total, err
	} else {
		total += n
	}
	return total, nil
}

func (t *Transaction) DecodeScale(dec *scale.Decoder) (total int, err error) {
	if n, err := t.RawTx.DecodeScale(dec); err != nil {
		return total, err
	} else {
		total += n
	}
	if field, n, err := scale.DecodeOption[TxHeader](dec); err != nil {
		return total, err
	} else {
		total += n
		t.TxHeader = field
	}
	return total, nil
}

func (t *Reward) EncodeScale(enc *scale.Encoder) (total int, err error) {
	if n, err := t.Layer.EncodeScale(enc); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.EncodeCompact64(enc, uint64(t.TotalReward)); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.EncodeCompact64(enc, uint64(t.LayerReward)); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.EncodeByteArray(enc, t.Coinbase[:]); err != nil {
		return total, err
	} else {
		total += n
	}
	return total, nil
}

func (t *Reward) DecodeScale(dec *scale.Decoder) (total int, err error) {
	if n, err := t.Layer.DecodeScale(dec); err != nil {
		return total, err
	} else {
		total += n
	}
	if field, n, err := scale.DecodeCompact64(dec); err != nil {
		return total, err
	} else {
		total += n
		t.TotalReward = uint64(field)
	}
	if field, n, err := scale.DecodeCompact64(dec); err != nil {
		return total, err
	} else {
		total += n
		t.LayerReward = uint64(field)
	}
	if n, err := scale.DecodeByteArray(dec, t.Coinbase[:]); err != nil {
		return total, err
	} else {
		total += n
	}
	return total, nil
}

func (t *RawTx) EncodeScale(enc *scale.Encoder) (total int, err error) {
	if n, err := scale.EncodeByteArray(enc, t.ID[:]); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.EncodeByteSlice(enc, t.Raw); err != nil {
		return total, err
	} else {
		total += n
	}
	return total, nil
}

func (t *RawTx) DecodeScale(dec *scale.Decoder) (total int, err error) {
	if n, err := scale.DecodeByteArray(dec, t.ID[:]); err != nil {
		return total, err
	} else {
		total += n
	}
	if field, n, err := scale.DecodeByteSlice(dec); err != nil {
		return total, err
	} else {
		total += n
		t.Raw = field
	}
	return total, nil
}
