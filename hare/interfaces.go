package hare

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interfaces.go

type layerPatrol interface {
	SetHareInCharge(types.LayerID)
	CompleteHare(types.LayerID)
}

// Rolacle is the roles oracle provider.
type Rolacle interface {
	Validate(context.Context, types.LayerID, uint32, int, types.NodeID, []byte, uint16) (bool, error)
	CalcEligibility(context.Context, types.LayerID, uint32, int, types.NodeID, []byte) (uint16, error)
	Proof(context.Context, types.LayerID, uint32) ([]byte, error)
	IsIdentityActiveOnConsensusView(context.Context, string, types.LayerID) (bool, error)
}

type meshProvider interface {
	AddBlockWithTXs(context.Context, *types.Block) error
	GetBallot(types.BallotID) (*types.Ballot, error)
	ProcessLayerPerHareOutput(context.Context, types.LayerID, types.BlockID) error
	RecordCoinflip(context.Context, types.LayerID, bool)
}

type proposalProvider interface {
	LayerProposals(types.LayerID) ([]*types.Proposal, error)
	GetProposals([]types.ProposalID) ([]*types.Proposal, error)
	DelProposals([]types.ProposalID) error
}

type blockGenerator interface {
	GenerateBlock(context.Context, types.LayerID, []*types.Proposal) (*types.Block, error)
}

type identityProvider interface {
	GetIdentity(string) (types.NodeID, error)
}

// stateQuerier provides a query to check if an Ed public key is active on the current consensus view.
// It returns true if the identity is active and false otherwise.
// An error is set iff the identity could not be checked for activeness.
type stateQuerier interface {
	IsIdentityActiveOnConsensusView(context.Context, string, types.LayerID) (bool, error)
}
