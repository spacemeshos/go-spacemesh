package weakcoin

import (
	"math/rand"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// Mock mocks weak coin.
type Mock struct{}

func (m Mock) PublishProposal(epoch types.EpochID, round types.RoundID) error {
	return nil
}

func (m Mock) Get(epoch types.EpochID, round types.RoundID) bool {
	if rand.Intn(2) == 0 {
		return false
	}

	return true
}

func (m Mock) HandleSerializedMessage(data service.GossipMessage, sync service.Fetcher) {
	return
}
