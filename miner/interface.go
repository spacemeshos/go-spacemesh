package miner

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type proposalOracle interface {
	GetProposalEligibility(types.LayerID, types.Beacon) (types.ATXID, []types.ATXID, []types.VotingEligibilityProof, error)
}

type meshProvider interface {
	AddBlockWithTxs(context.Context, *types.Block) error
}

type txPool interface {
	SelectTopNTransactions(numOfTxs int, getState func(addr types.Address) (nonce, balance uint64, err error)) ([]types.TransactionID, []*types.Transaction, error)
}

type baseBallotProvider interface {
	BaseBallot(context.Context) (types.BallotID, [][]types.BlockID, error)
}

type activationDB interface {
	GetNodeAtxIDForEpoch(nodeID types.NodeID, targetEpoch types.EpochID) (types.ATXID, error)
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
	GetEpochWeight(types.EpochID) (uint64, []types.ATXID, error)
}

type projector interface {
	GetProjection(types.Address) (nonce, balance uint64, err error)
}
