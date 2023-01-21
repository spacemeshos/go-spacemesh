package proposals

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type meshProvider interface {
	AddBallot(context.Context, *types.Ballot) (*types.MalfeasanceProof, error)
	AddTXsFromProposal(context.Context, types.LayerID, types.ProposalID, []types.TransactionID) error
}

type eligibilityValidator interface {
	CheckEligibility(context.Context, *types.Ballot, types.VRFPostIndex) (bool, error)
}

type ballotDecoder interface {
	DecodeBallot(*types.Ballot) (*tortoise.DecodedBallot, error)
	StoreBallot(*tortoise.DecodedBallot) error
}

type vrfVerifier interface {
	Verify(types.NodeID, []byte, []byte) bool
}
