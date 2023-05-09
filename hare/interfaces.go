package hare

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interfaces.go

type layerPatrol interface {
	SetHareInCharge(types.LayerID)
}

// Rolacle is the roles oracle provider.
type Rolacle interface {
	Validate(context.Context, types.LayerID, uint32, int, types.NodeID, types.VrfSignature, uint16) (bool, error)
	CalcEligibility(context.Context, types.LayerID, uint32, int, types.NodeID, types.VRFPostIndex, types.VrfSignature) (uint16, error)
	Proof(context.Context, types.VRFPostIndex, types.LayerID, uint32) (types.VrfSignature, error)
	IsIdentityActiveOnConsensusView(context.Context, types.NodeID, types.LayerID) (bool, error)
}

// stateQuerier provides a query to check if an Ed public key is active on the current consensus view.
// It returns true if the identity is active and false otherwise.
// An error is set iff the identity could not be checked for activeness.
type stateQuerier interface {
	IsIdentityActiveOnConsensusView(context.Context, types.NodeID, types.LayerID) (bool, error)
}

type mesh interface {
	VRFNonce(types.NodeID, types.EpochID) (types.VRFPostIndex, error)
	GetEpochAtx(types.EpochID, types.NodeID) (*types.ActivationTxHeader, error)
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
	Proposals(types.LayerID) ([]*types.Proposal, error)
	Ballot(types.BallotID) (*types.Ballot, error)
	IsMalicious(types.NodeID) (bool, error)
	AddMalfeasanceProof(types.NodeID, *types.MalfeasanceProof, *sql.Tx) error
	GetMalfeasanceProof(nodeID types.NodeID) (*types.MalfeasanceProof, error)
}

type weakCoin interface {
	Set(types.LayerID, bool) error
}
