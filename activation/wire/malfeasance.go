package wire

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate scalegen

// ProofType is an identifier for the type of proof that is encoded in the ATXProof.
type ProofType byte

const (
	DoublePublish ProofType = iota + 1
	DoubleMarry
	DoubleMerge
	InvalidPrevious
	InvalidPost
)

type ATXProof struct {
	// LayerID is the layer in which the proof was created. This can be used to implement different versions of the ATX
	// proof in the future.
	Layer types.LayerID
	// ProofType is the type of proof that is being provided.
	ProofType ProofType
	// Proof is the actual proof. Its type depends on the ProofType.
	Proof []byte `scale:"max=1048576"` // max size of proof is 1MiB
}
