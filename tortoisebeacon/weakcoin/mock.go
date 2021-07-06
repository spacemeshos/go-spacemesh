package weakcoin

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

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
func (m ValueMock) PublishProposal(ctx context.Context, epoch types.EpochID, round types.RoundID) error {
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
