package v2beta1

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/identity"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
)

//go:generate mockgen -typed -package=v2beta1 -destination=./mocks.go -source=./interface.go

type malfeasanceInfo interface {
	Info(ctx context.Context, nodeID types.NodeID) (map[string]string, error)
}

type identityState interface {
	All(ops builder.Operations) map[types.NodeID][]identity.StateInfo
	AllProposals() map[types.NodeID][]*types.Proposal
	AllEligibilities() map[types.NodeID]map[types.LayerID][]types.VotingEligibility
}
