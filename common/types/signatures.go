package types

import (
	"encoding/hex"

	"github.com/spacemeshos/go-scale"
)

const (
	EdSignatureSize  = 64
	VrfSignatureSize = 80
)

type EdSignature [EdSignatureSize]byte

// EmptyEdSignature is a canonical empty EdSignature.
var EmptyEdSignature EdSignature

// EncodeScale implements scale codec interface.
func (s *EdSignature) EncodeScale(encoder *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(encoder, s[:])
}

// DecodeScale implements scale codec interface.
func (s *EdSignature) DecodeScale(decoder *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(decoder, s[:])
}

// String returns a string representation of the Signature, for logging purposes.
// It implements the Stringer interface.
func (s EdSignature) String() string {
	return hex.EncodeToString(s.Bytes())
}

// Bytes returns the byte representation of the Signature.
func (s *EdSignature) Bytes() []byte {
	if s == nil {
		return nil
	}
	return s[:]
}

type VrfSignature [VrfSignatureSize]byte

// EmptyVrfSignature is a canonical empty VrfSignature.
var EmptyVrfSignature VrfSignature

// String returns a string representation of the Signature, for logging purposes.
// It implements the Stringer interface.
func (s VrfSignature) String() string {
	return hex.EncodeToString(s.Bytes())
}

// Bytes returns the byte representation of the Signature.
func (s *VrfSignature) Bytes() []byte {
	if s == nil {
		return nil
	}
	return s[:]
}

// Cmp compares s and x and returns:
//
//	-1 if s <  x
//	 0 if s == x
//	+1 if s >  x
//
// The comparison is done in little endian order.
// Additionally, if x is nil, -1 is returned.
func (s *VrfSignature) Cmp(x *VrfSignature) int {
	if x == nil {
		return -1
	}

	// VRF signatures are little endian, so we need to compare in reverse order.
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] < x[i] {
			return -1
		}
		if s[i] > x[i] {
			return 1
		}
	}
	return 0
}

// LSB returns the least significant byte of the signature.
func (s *VrfSignature) LSB() byte {
	return s[0]
}
