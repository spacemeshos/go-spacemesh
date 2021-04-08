package tortoisebeacon

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// TBProposalProtocol is Tortoise Beacon proposal Gossip protocol name.
const TBProposalProtocol = "TBProposalGossip"

// TBVotingProtocol is Tortoise Beacon voting Gossip protocol name.
const TBVotingProtocol = "TBVotingGossip"

// TBWeakCoinProtocol is Tortoise Beacon Weak Coin Gossip protocol name.
const TBWeakCoinProtocol = "TBWeakCoinGossip"

// HandleProposalMessage defines method to handle Tortoise Beacon proposal Messages from gossip.
func (tb *TortoiseBeacon) HandleProposalMessage(data service.GossipMessage, sync service.Fetcher) {
	tb.Log.With().Info("New proposal message",
		log.String("from", data.Sender().String()))

	var m ProposalMessage
	if err := types.BytesToInterface(data.Bytes(), &m); err != nil {
		tb.Log.With().Error("Received invalid proposal message",
			log.String("message", string(data.Bytes())),
			log.Err(err))

		return
	}

	if err := tb.handleProposalMessage(m); err != nil {
		tb.Log.With().Error("Failed to handle proposal message",
			log.String("message", m.String()),
			log.Err(err))

		return
	}

	data.ReportValidation(TBProposalProtocol)
}

// HandleVotingMessage defines method to handle Tortoise Beacon proposal Messages from gossip.
func (tb *TortoiseBeacon) HandleVotingMessage(data service.GossipMessage, sync service.Fetcher) {
	from := data.Sender()

	tb.Log.With().Info("New voting message",
		log.String("from", from.String()))

	var m VotingMessage
	if err := types.BytesToInterface(data.Bytes(), &m); err != nil {
		tb.Log.With().Error("Received invalid voting message",
			log.String("message", string(data.Bytes())),
			log.Err(err))

		return
	}

	if err := tb.handleVotingMessage(from, m); err != nil {
		tb.Log.With().Error("Failed to handle voting message",
			log.String("message", m.String()),
			log.Err(err))

		return
	}

	data.ReportValidation(TBVotingProtocol)
}

// HandleWeakCoinMessage defines method to handle Tortoise Beacon Weak Coin Messages from gossip.
func (tb *TortoiseBeacon) HandleWeakCoinMessage(data service.GossipMessage, sync service.Fetcher) {
	var m WeakCoinMessage

	err := types.BytesToInterface(data.Bytes(), &m)
	if err != nil {
		tb.Log.With().Error("Received invalid weak coin message",
			log.String("message", string(data.Bytes())),
			log.Err(err))

		return
	}

	if err := tb.handleWeakCoinMessage(m); err != nil {
		tb.Log.With().Error("Failed to handle weak coin message",
			log.String("message", m.String()),
			log.Err(err))

		return
	}

	data.ReportValidation(TBWeakCoinProtocol)
}
