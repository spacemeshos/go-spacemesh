package beacon

import (
	"context"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"

	"github.com/spacemeshos/go-spacemesh/beacon/weakcoin"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type activationDB interface {
	GetEpochWeight(types.EpochID) (uint64, []types.ATXID, error)
	GetNodeAtxIDForEpoch(types.NodeID, types.EpochID) (types.ATXID, error)
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
	GetAtxTimestamp(types.ATXID) (time.Time, error)
}

type coin interface {
	StartEpoch(context.Context, types.EpochID, weakcoin.UnitAllowances)
	StartRound(context.Context, types.RoundID) error
	FinishRound(context.Context)
	Get(context.Context, types.EpochID, types.RoundID) bool
	FinishEpoch(context.Context, types.EpochID)
	HandleProposal(context.Context, peer.ID, []byte) pubsub.ValidationResult
}

type eligibilityChecker interface {
	IsProposalEligible([]byte) bool
}

type layerClock interface {
	Subscribe() timesync.LayerTimer
	Unsubscribe(timesync.LayerTimer)
	LayerToTime(types.LayerID) time.Time
	GetCurrentLayer() types.LayerID
}
