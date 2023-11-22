package util

import "encoding/base64"

type Base64Enc struct {
	inner []byte
}

func (b *Base64Enc) UnmarshalText(text []byte) error {
	v, err := base64.StdEncoding.DecodeString(string(text))
	if err != nil {
		return err
	}
	b.inner = v
	return nil
}

func (b *Base64Enc) Bytes() []byte {
	return b.inner
}

func Base64FromString(s string) (Base64Enc, error) {
	b := Base64Enc{}
	if err := b.UnmarshalText([]byte(s)); err != nil {
		return Base64Enc{}, err
	}
	return b, nil
}

func MustBase64FromString(s string) Base64Enc {
	b, err := Base64FromString(s)
	if err != nil {
		panic(err)
	}
	return b
}
