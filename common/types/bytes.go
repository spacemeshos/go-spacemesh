package types

import "github.com/spacemeshos/go-scale"

// Bytes64 is 64 byte array.
type Bytes64 [64]byte

// EncodeScale implements scale codec interface.
func (b *Bytes64) EncodeScale(encoder *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(encoder, b[:])
}

// DecodeScale implements scale codec interface.
func (b *Bytes64) DecodeScale(decoder *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(decoder, b[:])
}
