package v2alpha1

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/identity"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
)

//go:generate mockgen -typed -package=v2alpha1 -destination=./mocks.go -source=./interface.go

type malfeasanceInfo interface {
	Info(data []byte) (map[string]string, error)
}

type identityState interface {
	All() map[types.NodeID][]identity.StateInfo
	AllByOps(ops builder.Operations) map[types.NodeID][]identity.StateInfo
	AllProposals() map[types.NodeID][]*types.Proposal
	AllEligibilities() map[types.NodeID]map[types.LayerID][]types.VotingEligibility
}
