package wire

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate scalegen

// TODO(mafa): convert existing malfeasance proofs to new format.
type MalfeasanceProofV2 struct {
	// for network upgrade
	Layer types.LayerID
	Proof []byte
}
