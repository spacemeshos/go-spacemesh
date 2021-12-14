package proposals

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type atxDB interface {
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
}

type mesh interface {
	HasProposal(types.ProposalID) bool
	AddProposalWithTxs(context.Context, *types.Proposal) error
	HasBallot(types.BallotID) bool
	AddBallot(*types.Ballot) error
	GetBallot(types.BallotID) (*types.Ballot, error)
}

type eligibilityValidator interface {
	CheckEligibility(context.Context, *types.Ballot) (bool, error)
}
