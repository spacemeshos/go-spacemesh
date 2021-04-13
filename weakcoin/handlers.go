package weakcoin

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// HandleSerializedMessage defines method to handle Tortoise Beacon Weak Coin Messages from gossip.
func (wc *weakCoin) HandleSerializedMessage(data service.GossipMessage, sync service.Fetcher) {
	var m Message

	if err := types.BytesToInterface(data.Bytes(), &m); err != nil {
		wc.Log.With().Error("Received invalid weak coin message",
			log.String("message", string(data.Bytes())),
			log.Err(err))

		return
	}

	if err := wc.handleWeakCoinMessage(m); err != nil {
		wc.Log.With().Error("Failed to handle weak coin message",
			log.String("message", m.String()),
			log.Err(err))

		return
	}

	data.ReportValidation(GossipProtocol)
}

func (wc *weakCoin) handleWeakCoinMessage(m Message) error {
	pair := epochRoundPair{
		EpochID: m.Epoch,
		Round:   m.Round,
	}

	wc.proposalsMu.Lock()
	defer wc.proposalsMu.Unlock()

	wc.proposals[pair] = append(wc.proposals[pair], m.Proposal)

	return nil
}
