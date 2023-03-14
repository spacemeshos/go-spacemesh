package miner

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

//go:generate mockgen -package=miner -destination=./mocks.go -source=./interface.go

type proposalOracle interface {
	ProposalEligibility(types.LayerID, types.Beacon, types.VRFPostIndex) (*EpochEligibility, error)
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

type mesh interface {
	GetMalfeasanceProof(nodeID types.NodeID) (*types.MalfeasanceProof, error)
}

type layerClock interface {
	AwaitLayer(layerID types.LayerID) <-chan struct{}
	CurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}
