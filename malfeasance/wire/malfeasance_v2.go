package wire

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate scalegen

// TODO(mafa): create proofs for existing malfeasance proofs in new format.
type MalfeasanceProofV2 struct {
	// for network upgrade
	Layer types.LayerID
	Proof []byte `scale:"max=1048576"` // max size of proof is 1MiB
}
