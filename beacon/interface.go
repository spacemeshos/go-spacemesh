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
	StartRound(context.Context, types.RoundID, *types.VRFPostIndex) error
	FinishRound(context.Context)
	Get(context.Context, types.EpochID, types.RoundID) (bool, error)
	FinishEpoch(context.Context, types.EpochID)
	HandleProposal(context.Context, p2p.Peer, []byte) pubsub.ValidationResult
}

type eligibilityChecker interface {
	PassThreshold([]byte) bool
	PassStrictThreshold([]byte) bool
}

type layerClock interface {
	LayerToTime(types.LayerID) time.Time
	CurrentLayer() types.LayerID
	AwaitLayer(types.LayerID) chan struct{}
}

type vrfSigner interface {
	Sign(msg []byte) []byte
	PublicKey() *signing.PublicKey
	LittleEndian() bool
}

type vrfVerifier interface {
	Verify(nodeID types.NodeID, msg, sig []byte) bool
}

type nonceFetcher interface {
	VRFNonce(types.NodeID, types.EpochID) (types.VRFPostIndex, error)
}
