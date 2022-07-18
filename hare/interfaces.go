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
	Validate(ctx context.Context,
		layerID types.LayerID, opIndex uint32, expectedCommitteeSize int,
		nodeID types.NodeID, roleProof []byte, eligibilityCount uint16,
	) (bool, error)
	CalcEligibility(ctx context.Context,
		layerID types.LayerID, opIndex uint32, committeeSize int,
		nodeID types.NodeID, vrfSig []byte,
	) (uint16, error)
	Proof(ctx context.Context,
		layerID types.LayerID, opIndex uint32,
	) ([]byte, error)
	IsIdentityActiveOnConsensusView(ctx context.Context,
		nodeID types.NodeID, layerID types.LayerID,
	) (bool, error)
}

type meshProvider interface {
	AddBlockWithTXs(context.Context, *types.Block) error
	ProcessLayerPerHareOutput(context.Context, types.LayerID, types.BlockID) error
}

type blockGenerator interface {
	GenerateBlock(context.Context, types.LayerID, []*types.Proposal) (*types.Block, error)
}

// stateQuerier provides a query to check if an Ed public key is active on the current consensus view.
// It returns true if the identity is active and false otherwise.
// An error is set iff the identity could not be checked for activeness.
type stateQuerier interface {
	IsIdentityActiveOnConsensusView(context.Context, types.NodeID, types.LayerID) (bool, error)
}
