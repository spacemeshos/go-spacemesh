package types

import (
	"encoding/hex"

	"github.com/spacemeshos/go-scale"
)

const (
	EdSignatureSize = 64
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

// String returns a string representation of the NodeID, for logging purposes.
// It implements the Stringer interface.
func (s *EdSignature) String() string {
	return hex.EncodeToString(s.Bytes())
}

// Bytes returns the byte representation of the Edwards public key.
func (s *EdSignature) Bytes() []byte {
	return s[:]
}
