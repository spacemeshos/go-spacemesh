package wire

import (
	"context"

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
}
