package proposals

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type atxDB interface {
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
}

type ballotDB interface {
	GetBallot(types.BallotID) (*types.Ballot, error)
}

type meshDB interface {
	AddTXsFromProposal(context.Context, types.LayerID, types.ProposalID, []types.TransactionID) error
	HasBallot(types.BallotID) bool
	AddBallot(*types.Ballot) error
	GetBallot(types.BallotID) (*types.Ballot, error)
	GetBlockLayer(types.BlockID) (types.LayerID, error)
	SetIdentityMalicious(*signing.PublicKey) error
}

type proposalDB interface {
	HasProposal(types.ProposalID) bool
	AddProposal(context.Context, *types.Proposal) error
}

type eligibilityValidator interface {
	CheckEligibility(context.Context, *types.Ballot) (bool, error)
}
