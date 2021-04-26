package weakcoin

import (
	"context"
	"math/rand"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// RandomMock mocks weak coin returning random value as weak coin.
type RandomMock struct{}

// OnRoundStarted should be called when round is started.
func (m RandomMock) OnRoundStarted(epoch types.EpochID, round types.RoundID) {
	return
}

// OnRoundFinished should be called when round is finished.
func (m RandomMock) OnRoundFinished(epoch types.EpochID, round types.RoundID) {
	return
}

// PublishProposal publishes a proposal.
func (m RandomMock) PublishProposal(epoch types.EpochID, round types.RoundID) error {
	return nil
}

// Get gets weak coin value.
func (m RandomMock) Get(epoch types.EpochID, round types.RoundID) bool {
	if rand.Intn(2) == 0 {
		return false
	}

	return true
}

// HandleSerializedMessage handles serialized message.
func (m RandomMock) HandleSerializedMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher) {
	return
}

// ValueMock mocks weak coin returning set value.
type ValueMock struct {
	Value bool
}

// OnRoundStarted should be called when round is started.
func (m ValueMock) OnRoundStarted(epoch types.EpochID, round types.RoundID) {
}

// OnRoundFinished should be called when round is finished.
func (m ValueMock) OnRoundFinished(epoch types.EpochID, round types.RoundID) {
}

// PublishProposal publishes a proposal.
func (m ValueMock) PublishProposal(epoch types.EpochID, round types.RoundID) error {
	return nil
}

// Get gets weak coin value.
func (m ValueMock) Get(epoch types.EpochID, round types.RoundID) bool {
	return m.Value
}

// HandleSerializedMessage handles serialized message.
func (m ValueMock) HandleSerializedMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher) {
	return
}
