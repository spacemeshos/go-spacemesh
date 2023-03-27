package types

import "github.com/spacemeshos/go-scale"

const (
	EdSignatureSize  = 64
	VrfSignatureSize = 80
)

type EdSignature [EdSignatureSize]byte

// EncodeScale implements scale codec interface.
func (b *EdSignature) EncodeScale(encoder *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(encoder, b[:])
}

// DecodeScale implements scale codec interface.
func (b *EdSignature) DecodeScale(decoder *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(decoder, b[:])
}

type VrfSignature [VrfSignatureSize]byte
