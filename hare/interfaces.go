package hare

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./interfaces.go

type layerPatrol interface {
	SetHareInCharge(types.LayerID)
}

// Rolacle is the roles oracle provider.
type Rolacle interface {
	Validate(context.Context, types.LayerID, uint32, int, types.NodeID, types.VrfSignature, uint16) (bool, error)
	CalcEligibility(context.Context, types.LayerID, uint32, int, types.NodeID, types.VrfSignature) (uint16, error)
	Proof(context.Context, types.LayerID, uint32) (types.VrfSignature, error)
	IsIdentityActiveOnConsensusView(context.Context, types.NodeID, types.LayerID) (bool, error)
}

// stateQuerier provides a query to check if an Ed public key is active on the current consensus view.
// It returns true if the identity is active and false otherwise.
// An error is set iff the identity could not be checked for activeness.
type stateQuerier interface {
	IsIdentityActiveOnConsensusView(context.Context, types.NodeID, types.LayerID) (bool, error)
}

type mesh interface {
	GetEpochAtx(types.EpochID, types.NodeID) (*types.ActivationTxHeader, error)
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
	Proposals(types.LayerID) ([]*types.Proposal, error)
	Ballot(types.BallotID) (*types.Ballot, error)
	GetMalfeasanceProof(types.NodeID) (*types.MalfeasanceProof, error)
	Cache() *datastore.CachedDB
}

type weakCoin interface {
	Set(types.LayerID, bool) error
}
