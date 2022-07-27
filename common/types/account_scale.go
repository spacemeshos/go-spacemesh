// Code generated by github.com/spacemeshos/go-scale/scalegen. DO NOT EDIT.

package types

import (
	"github.com/spacemeshos/go-scale"
)

func (t *Account) EncodeScale(enc *scale.Encoder) (total int, err error) {
	if n, err := t.Layer.EncodeScale(enc); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.EncodeByteArray(enc, t.Address[:]); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.EncodeBool(enc, t.Initialized); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.EncodeCompact64(enc, t.Nonce); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.EncodeCompact64(enc, t.Balance); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.EncodeOption(enc, t.Template); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.EncodeByteSlice(enc, t.State); err != nil {
		return total, err
	} else {
		total += n
	}
	return total, nil
}

func (t *Account) DecodeScale(dec *scale.Decoder) (total int, err error) {
	if n, err := t.Layer.DecodeScale(dec); err != nil {
		return total, err
	} else {
		total += n
	}
	if n, err := scale.DecodeByteArray(dec, t.Address[:]); err != nil {
		return total, err
	} else {
		total += n
	}
	if field, n, err := scale.DecodeBool(dec); err != nil {
		return total, err
	} else {
		total += n
		t.Initialized = field
	}
	if field, n, err := scale.DecodeCompact64(dec); err != nil {
		return total, err
	} else {
		total += n
		t.Nonce = field
	}
	if field, n, err := scale.DecodeCompact64(dec); err != nil {
		return total, err
	} else {
		total += n
		t.Balance = field
	}
	if field, n, err := scale.DecodeOption[Address](dec); err != nil {
		return total, err
	} else {
		total += n
		t.Template = field
	}
	if field, n, err := scale.DecodeByteSlice(dec); err != nil {
		return total, err
	} else {
		total += n
		t.State = field
	}
	return total, nil
}
