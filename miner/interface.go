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
	SelectTXsForProposal(int) ([]types.TransactionID, []*types.Transaction, error)
}

type votesEncoder interface {
	EncodeVotes(context.Context, ...tortoise.EncodeVotesOpts) (*types.Votes, error)
}

type activationDB interface {
	GetNodeAtxIDForEpoch(nodeID types.NodeID, targetEpoch types.EpochID) (types.ATXID, error)
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
	GetEpochWeight(types.EpochID) (uint64, []types.ATXID, error)
}
