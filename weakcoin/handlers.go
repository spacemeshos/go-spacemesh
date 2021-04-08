package weakcoin

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// HandleSerializedMessage defines method to handle Tortoise Beacon Weak Coin Messages from gossip.
func (wc *weakCoin) HandleSerializedMessage(data service.GossipMessage, sync service.Fetcher) {
	var m Message

	err := types.BytesToInterface(data.Bytes(), &m)
	if err != nil {
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
	// TODO(nkryuchkov): implement
	return nil
}
