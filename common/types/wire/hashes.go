package wire

import "github.com/spacemeshos/go-scale"

// Hash32 represents the 32-byte blake3 hash of arbitrary data.
type Hash32 [32]byte

// Hash20 represents the 20-byte blake3 hash of arbitrary data.
type Hash20 [20]byte

// EncodeScale implements scale codec interface.
func (h *Hash32) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(e, h[:])
}

// DecodeScale implements scale codec interface.
func (h *Hash32) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, h[:])
}
