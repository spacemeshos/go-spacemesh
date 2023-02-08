package miner

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

//go:generate mockgen -package=miner -destination=./mocks.go -source=./interface.go

type proposalOracle interface {
	GetProposalEligibility(types.LayerID, types.Beacon, types.VRFPostIndex) (types.ATXID, []types.ATXID, []types.VotingEligibility, error)
}

type conservativeState interface {
	SelectProposalTXs(types.LayerID, int) []types.TransactionID
}

type votesEncoder interface {
	LatestComplete() types.LayerID
	TallyVotes(context.Context, types.LayerID)
	EncodeVotes(context.Context, ...tortoise.EncodeVotesOpts) (*types.Opinion, error)
}

type nonceFetcher interface {
	VRFNonce(types.NodeID, types.EpochID) (types.VRFPostIndex, error)
}

type layerClock interface {
	AwaitLayer(layerID types.LayerID) chan struct{}
	CurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}
