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

	// AllowNoRefATXs returns true if the proof type is valid without reference ATXs proving the existence of the
	// malicious identity.
	//
	// To avoid spamming of malfeasance proofs for identities that do not exist, by default all proofs require reference
	// ATXs (syntactically valid ATXs published by the malicious identity) to be provided. This way any identity that
	// the network considers malicious must have been in good standing at some point before the malicious behavior.
	//
	// For some malfeasance proofs this requirement is not necessary, for example invalid post proofs. Since those
	// require the creator of the proof to show that some labels in the post are valid and some invalid. If all are
	// invalid, the ATX would be considered syntactically invalid by the network anyway and a proof is not needed.
	// In contrast if we would require a reference ATX we couldn't proof an invalid post in an initial ATX of any new
	// identity.
	AllowNoRefATXs() bool

	Type() ProofType
	TypeName() string
	Info() map[string]string
	Valid(ctx context.Context, malHandler MalfeasanceValidator) (types.NodeID, error)
}
