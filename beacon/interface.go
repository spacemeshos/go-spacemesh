package beacon

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate mockgen -package=beacon -destination=./mocks.go -source=./interface.go

type coin interface {
	StartEpoch(context.Context, types.EpochID)
	StartRound(context.Context, types.RoundID, *types.VRFPostIndex)
	FinishRound(context.Context)
	Get(context.Context, types.EpochID, types.RoundID) (bool, error)
	FinishEpoch(context.Context, types.EpochID)
	HandleProposal(context.Context, p2p.Peer, []byte) pubsub.ValidationResult
}

type eligibilityChecker interface {
	PassThreshold(types.VrfSignature) bool
	PassStrictThreshold(types.VrfSignature) bool
}

type layerClock interface {
	LayerToTime(types.LayerID) time.Time
	CurrentLayer() types.LayerID
	AwaitLayer(types.LayerID) chan struct{}
}

type vrfSigner interface {
	Sign(msg []byte) types.VrfSignature
	PublicKey() *signing.PublicKey
	LittleEndian() bool
}

type vrfVerifier interface {
	Verify(types.NodeID, []byte, types.VrfSignature) bool
}

type nonceFetcher interface {
	VRFNonce(types.NodeID, types.EpochID) (types.VRFPostIndex, error)
}
