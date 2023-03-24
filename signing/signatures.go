package signing

import "github.com/spacemeshos/go-scale"

const (
	EdSignatureSize  = 64
	VrfSignatureSize = 80
)

type EdSignature [EdSignatureSize]byte

// EncodeScale implements scale codec interface.
func (sig *EdSignature) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(e, sig[:])
}

// DecodeScale implements scale codec interface.
func (sig *EdSignature) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, sig[:])
}

type VrfSignature [VrfSignatureSize]byte

// EncodeScale implements scale codec interface.
func (sig *VrfSignature) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(e, sig[:])
}

// DecodeScale implements scale codec interface.
func (sig *VrfSignature) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, sig[:])
}
