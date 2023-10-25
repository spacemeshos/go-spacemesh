package proposals

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

//go:generate mockgen -typed -package=proposals -destination=./mocks.go -source=./interface.go

type meshProvider interface {
	ProcessedLayer() types.LayerID
	AddBallot(context.Context, *types.Ballot) (*types.MalfeasanceProof, error)
	AddTXsFromProposal(context.Context, types.LayerID, types.ProposalID, []types.TransactionID) error
}

type eligibilityValidator interface {
	CheckEligibility(context.Context, *types.Ballot, []types.ATXID) (bool, error)
}

type tortoiseProvider interface {
	GetBallot(types.BallotID) *tortoise.BallotData
	DecodeBallot(*types.BallotTortoiseData) (*tortoise.DecodedBallot, error)
	StoreBallot(*tortoise.DecodedBallot) error
}

type vrfVerifier interface {
	Verify(types.NodeID, []byte, types.VrfSignature) bool
}

type layerClock interface {
	CurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}
