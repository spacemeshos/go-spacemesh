package tortoisebeacon

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// TBProposalProtocol is Tortoise Beacon proposal Gossip protocol name.
const TBProposalProtocol = "TBProposalGossip"

// TBVotingProtocol is Tortoise Beacon voting Gossip protocol name.
const TBVotingProtocol = "TBVotingGossip"

// HandleSerializedProposalMessage defines method to handle Tortoise Beacon proposal Messages from gossip.
func (tb *TortoiseBeacon) HandleSerializedProposalMessage(data service.GossipMessage, sync service.Fetcher) {
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

func (tb *TortoiseBeacon) handleProposalMessage(m ProposalMessage) error {
	epoch := m.Epoch()

	mt := tb.classifyMessage(m, epoch)

	switch mt {
	case TimelyMessage:
		tb.Log.With().Info("Received timely ProposalMessage",
			log.Uint64("epoch_id", uint64(m.Epoch())),
			log.String("message", m.String()))

		tb.timelyProposalsMu.Lock()

		if _, ok := tb.timelyProposals[epoch]; !ok {
			tb.timelyProposals[epoch] = make(map[types.Hash32]struct{})
		}

		tb.timelyProposals[epoch][m.Proposals().Hash()] = struct{}{}

		tb.timelyProposalsMu.Unlock()

		return nil

	case DelayedMessage:
		tb.Log.With().Info("Received delayed ProposalMessage",
			log.Uint64("epoch_id", uint64(m.Epoch())),
			log.String("message", m.String()))

		tb.delayedProposalsMu.Lock()

		if _, ok := tb.delayedProposals[epoch]; !ok {
			tb.delayedProposals[epoch] = make(map[types.Hash32]struct{})
		}

		tb.delayedProposals[epoch][m.Proposals().Hash()] = struct{}{}

		tb.delayedProposalsMu.Unlock()

		return nil

	case LateMessage:
		tb.Log.With().Warning("Received late ProposalMessage",
			log.Uint64("epoch_id", uint64(m.Epoch())),
			log.Int("type", int(mt)))

		return nil

	default:
		tb.Log.With().Info("Received ProposalMessage of unknown type",
			log.Uint64("epoch_id", uint64(m.Epoch())),
			log.String("message", m.String()))

		return ErrUnknownMessageType
	}
}

// HandleSerializedVotingMessage defines method to handle Tortoise Beacon proposal Messages from gossip.
func (tb *TortoiseBeacon) HandleSerializedVotingMessage(data service.GossipMessage, sync service.Fetcher) {
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

func (tb *TortoiseBeacon) handleVotingMessage(from p2pcrypto.PublicKey, message VotingMessage) error {
	messageEpoch := message.Epoch()
	messageRound := message.Round()

	mt := tb.classifyMessage(message, messageEpoch)
	switch mt {
	case TimelyMessage:
		tb.Log.With().Info("Received timely VotingMessage, counting it",
			log.Uint64("epoch_id", uint64(message.Epoch())),
			log.Uint64("round", uint64(messageRound)),
			log.String("message", message.String()))

		thisRound := epochRoundPair{
			EpochID: messageEpoch,
			Round:   messageRound,
		}

		tb.votesMu.Lock()
		defer tb.votesMu.Unlock()

		if _, ok := tb.incomingVotes[thisRound]; !ok {
			tb.incomingVotes[thisRound] = make(votesPerPK)
		}

		votesFor := make(hashSet)
		votesAgainst := make(hashSet)

		for _, vote := range message.For() {
			votesFor[vote] = struct{}{}
		}

		for _, vote := range message.Against() {
			votesAgainst[vote] = struct{}{}
		}

		tb.incomingVotes[thisRound][from] = votesSetPair{
			VotesFor:     votesFor,
			VotesAgainst: votesAgainst,
		}

		return nil

	case DelayedMessage, LateMessage:
		tb.Log.With().Warning(fmt.Sprintf("Received %v VotingMessage, ignoring it", mt.String()),
			log.Uint64("epoch_id", uint64(message.Epoch())),
			log.Uint64("round", uint64(messageRound)),
			log.String("message", message.String()))

		return nil

	default:
		tb.Log.With().Info("Received VotingMessage of unknown type",
			log.Uint64("epoch_id", uint64(message.Epoch())),
			log.Uint64("round", uint64(messageRound)),
			log.Int("type", int(mt)))

		return ErrUnknownMessageType
	}
}

func (tb *TortoiseBeacon) classifyMessage(m message, epoch types.EpochID) MessageType {
	tb.currentRoundsMu.Lock()
	currentRound := tb.currentRounds[epoch]
	tb.currentRoundsMu.Unlock()

	// If ProposalMessage, messageRound == 1.
	// If VotingMessage, messageRound is extracted from message.
	messageRound := types.RoundID(1)
	if vm, ok := m.(VotingMessage); ok {
		messageRound = vm.Round()
	}

	classification := LateMessage

	switch {
	case currentRound-messageRound <= 1, currentRound < messageRound:
		classification = TimelyMessage
	case messageRound == currentRound-2:
		classification = DelayedMessage
	}

	tb.Log.With().Info(fmt.Sprintf("Message is considered %s", classification.String()),
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("message_epoch", uint64(m.Epoch())),
		log.Uint64("message_round", uint64(messageRound)),
		log.Uint64("current_round", uint64(currentRound)),
		log.String("message", m.String()))

	return classification
}
