package v2alpha1

import (
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -typed -package=v2alpha1 -destination=./mocks.go -source=./interface.go

type malfeasanceInfo interface {
	Info(data []byte) (map[string]string, error)
}

type identityState interface {
	All() map[types.NodeID][]activation.IdentityStateInfo
	AllProposals() map[types.NodeID][]*types.Proposal
	AllEligibilities() map[types.NodeID]map[types.LayerID][]types.VotingEligibility
}
