package weakcoin

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// HandleWeakCoinMessage defines method to handle Tortoise Beacon Weak Coin Messages from gossip.
func (wcg *weakCoinGenerator) HandleWeakCoinMessage(data service.GossipMessage, sync service.Fetcher) {
	var m WeakCoinMessage

	err := types.BytesToInterface(data.Bytes(), &m)
	if err != nil {
		wcg.Log.With().Error("Received invalid weak coin message",
			log.String("message", string(data.Bytes())),
			log.Err(err))

		return
	}

	if err := wcg.handleWeakCoinMessage(m); err != nil {
		wcg.Log.With().Error("Failed to handle weak coin message",
			log.String("message", m.String()),
			log.Err(err))

		return
	}

	data.ReportValidation(GossipProtocol)
}

func (wcg *weakCoinGenerator) handleWeakCoinMessage(m WeakCoinMessage) error {
	// TODO(nkryuchkov): implement
	return nil
}
