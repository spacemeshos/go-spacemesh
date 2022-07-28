package miner

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type proposalOracle interface {
	GetProposalEligibility(types.LayerID, types.Beacon) (types.ATXID, []types.ATXID, []types.VotingEligibilityProof, error)
}

type conservativeState interface {
	SelectProposalTXs(types.LayerID, int) []types.TransactionID
}

type votesEncoder interface {
	EncodeVotes(context.Context, ...tortoise.EncodeVotesOpts) (*types.Votes, error)
}
