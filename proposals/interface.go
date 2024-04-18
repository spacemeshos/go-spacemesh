package proposals

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

//go:generate mockgen -typed -package=proposals -destination=./mocks.go -source=./interface.go

type meshProvider interface {
	ProcessedLayer() types.LayerID
	AddBallot(context.Context, *types.Ballot) (*wire.MalfeasanceProof, error)
	AddTXsFromProposal(context.Context, types.LayerID, types.ProposalID, []types.TransactionID) error
}

type eligibilityValidator interface {
	CheckEligibility(context.Context, *types.Ballot, uint64) error
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

type proposalsConsumer interface {
	IsKnown(types.LayerID, types.ProposalID) bool
	OnProposal(p *types.Proposal) error
}
