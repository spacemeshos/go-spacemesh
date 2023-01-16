package miner

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

//go:generate mockgen -package=miner -destination=./mocks.go -source=./interface.go

type proposalOracle interface {
	GetProposalEligibility(types.LayerID, types.Beacon) (types.ATXID, []types.ATXID, []types.VotingEligibility, error)
}

type conservativeState interface {
	SelectProposalTXs(types.LayerID, int) []types.TransactionID
}

type votesEncoder interface {
	LatestComplete() types.LayerID
	TallyVotes(context.Context, types.LayerID)
	EncodeVotes(context.Context, ...tortoise.EncodeVotesOpts) (*types.Opinion, error)
}
