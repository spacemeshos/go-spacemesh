package proposals

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

//go:generate mockgen -package=proposals -destination=./mocks.go -source=./interface.go

type meshProvider interface {
	AddBallot(context.Context, *types.Ballot) (*types.MalfeasanceProof, error)
	AddTXsFromProposal(context.Context, types.LayerID, types.ProposalID, []types.TransactionID) error
}

type eligibilityValidator interface {
	CheckEligibility(context.Context, *types.Ballot) (bool, error)
}

type tortoiseProvider interface {
	GetBallot(types.BallotID) *tortoise.BallotData
	GetMissingActiveSet(types.EpochID, []types.ATXID) []types.ATXID
	DecodeBallot(*types.BallotTortoiseData) (*tortoise.DecodedBallot, error)
	StoreBallot(*tortoise.DecodedBallot) error
}

type vrfVerifier interface {
	Verify(types.NodeID, []byte, types.VrfSignature) bool
}

type nonceFetcher interface {
	VRFNonce(types.NodeID, types.EpochID) (types.VRFPostIndex, error)
}

type layerClock interface {
	CurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}
