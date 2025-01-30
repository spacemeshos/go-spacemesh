package wire

import (
	"context"
	"errors"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate mockgen -typed -package=wire -destination=./mocks.go -source=./interface.go

type MalfeasanceValidator interface {
	// PostIndex validates the given post against for the provided index.
	// It returns an error if the post is invalid.
	PostIndex(
		ctx context.Context,
		smesherID types.NodeID,
		commitment types.ATXID,
		post *types.Post,
		challenge []byte,
		numUnits uint32,
		idx int,
	) error

	// Signature validates the given signature against the given message and public key.
	Signature(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool

	// IdentityExists returns true if the given identity has published a valid ATX before.
	IdentityExists(nodeID types.NodeID) (bool, error)
}

var ErrUnknownIdentity = errors.New("unknown identity")

// Proof is an interface for all types of proofs that can be provided in an ATXProof.
// Generally the proof should be able to validate itself and be scale encoded.
type Proof interface {
	scale.Encodable
	scale.Decodable

	AllowNoRefATXs() bool
	Type() ProofType
	TypeName() string
	Info() map[string]string
	Valid(ctx context.Context, malHandler MalfeasanceValidator) (types.NodeID, error)
}
