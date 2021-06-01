package tortoisebeacon

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// TBProposalProtocol is Tortoise Beacon proposal Gossip protocol name.
const TBProposalProtocol = "TBProposalGossip"
const TBFirstVotingProtocol = "TBFirstVotingGossip"
const TBFollowingVotingProtocol = "TBFollowingVotingGossip"

var (
	// ErrMalformedProposal is returned if proposal message is malformed.
	ErrMalformedProposal = errors.New("malformed proposal message")
)

// HandleSerializedProposalMessage defines method to handle Tortoise Beacon proposal Messages from gossip.
// TODO(nkryuchkov): use context
func (tb *TortoiseBeacon) HandleSerializedProposalMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher) {
	tb.Log.With().Info("New proposal message",
		log.String("from", data.Sender().String()))

	var m ProposalMessage
	if err := types.BytesToInterface(data.Bytes(), &m); err != nil {
		tb.Log.With().Error("Received invalid proposal message",
			log.String("message", string(data.Bytes())),
			log.Err(err))

		return
	}

	if err := tb.handleProposalMessage(data.Sender(), m); err != nil {
		tb.Log.With().Error("Failed to handle proposal message",
			log.String("sender", data.Sender().String()),
			log.String("message", m.String()),
			log.Err(err))

		return
	}

	data.ReportValidation(TBProposalProtocol)
}

func (tb *TortoiseBeacon) handleProposalMessage(sender p2pcrypto.PublicKey, m ProposalMessage) error {
	epoch := tb.currentEpoch()

	currentEpochProposal, err := tb.calcProposal(epoch)
	if err != nil {
		return fmt.Errorf("calculate proposal: %w", err)
	}

	// Ensure that epoch is the same.
	ok := tb.vrfVerifier(sender.Bytes(), currentEpochProposal, m.VRFSignature)
	if !ok {
		return nil
	}

	epochWeight, _, err := tb.atxDB.GetEpochWeight(epoch)
	if err != nil {
		return fmt.Errorf("get epoch weight: %w", err)
	}

	exceeds, err := tb.proposalExceedsThreshold(m.VRFSignature, epochWeight)
	if err != nil {
		return fmt.Errorf("proposalExceedsThreshold: %w", err)
	}

	if exceeds {
		tb.Log.With().Warning("Rejected proposal message which exceeds threshold")
		return nil
	}

	nodeID := types.NodeID{Key: sender.String()}
	atxID, err := tb.atxDB.GetNodeAtxIDForEpoch(nodeID, epoch)
	if err != nil {
		return err
	}

	atxHeader, err := tb.atxDB.GetAtxHeader(atxID)
	if err != nil {
		return err
	}

	atxTimestamp, err := tb.atxDB.GetAtxTimestamp(atxID)
	if err != nil {
		return err
	}

	atxEpoch := atxHeader.PubLayerID.GetEpoch()
	nextEpochStart := tb.clock.LayerToTime((atxEpoch + 1).FirstLayer())

	switch {
	case atxTimestamp.Before(nextEpochStart):
		tb.Log.With().Info("Received valid proposal message",
			log.Uint64("epoch_id", uint64(epoch)),
			log.String("message", m.String()))

		tb.validProposalsMu.Lock()

		if _, ok := tb.validProposals[epoch]; !ok {
			tb.validProposals[epoch] = make(map[string]struct{})
		}

		tb.validProposals[epoch][util.Bytes2Hex(m.VRFSignature)] = struct{}{}

		tb.validProposalsMu.Unlock()

	case atxTimestamp.Before(nextEpochStart.Add(tb.votingRoundDuration)):
		tb.Log.With().Info("Received potentially valid proposal message",
			log.Uint64("epoch_id", uint64(epoch)),
			log.String("message", m.String()))

		tb.potentiallyValidProposalsMu.Lock()

		if _, ok := tb.potentiallyValidProposals[epoch]; !ok {
			tb.potentiallyValidProposals[epoch] = make(map[string]struct{})
		}

		tb.potentiallyValidProposals[epoch][util.Bytes2Hex(m.VRFSignature)] = struct{}{}

		tb.potentiallyValidProposalsMu.Unlock()

	default:
		tb.Log.With().Warning("Received invalid proposal message",
			log.Uint64("epoch_id", uint64(epoch)))
	}

	return nil
}

// HandleSerializedFirstVotingMessage defines method to handle Tortoise Beacon first voting Messages from gossip.
func (tb *TortoiseBeacon) HandleSerializedFirstVotingMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher) {
	from := data.Sender()

	tb.Log.With().Info("New voting message",
		log.String("from", from.String()))

	var m FirstVotingMessage
	if err := types.BytesToInterface(data.Bytes(), &m); err != nil {
		tb.Log.With().Error("Received invalid voting message",
			log.String("message", string(data.Bytes())),
			log.Err(err))

		return
	}

	if err := tb.handleFirstVotingMessage(ctx, from, m); err != nil {
		tb.Log.With().Error("Failed to handle voting message",
			log.Err(err))

		return
	}

	data.ReportValidation(TBFirstVotingProtocol)
}

// HandleSerializedFollowingVotingMessage defines method to handle Tortoise Beacon following voting Messages from gossip.
func (tb *TortoiseBeacon) HandleSerializedFollowingVotingMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher) {
	from := data.Sender()

	tb.Log.With().Info("New voting message",
		log.String("from", from.String()))

	var m FollowingVotingMessage
	if err := types.BytesToInterface(data.Bytes(), &m); err != nil {
		tb.Log.With().Error("Received invalid voting message",
			log.String("message", string(data.Bytes())),
			log.Err(err))

		return
	}

	if err := tb.handleFollowingVotingMessage(ctx, from, m); err != nil {
		tb.Log.With().Error("Failed to handle voting message",
			log.Err(err))

		return
	}

	data.ReportValidation(TBFollowingVotingProtocol)
}

func (tb *TortoiseBeacon) handleFirstVotingMessage(ctx context.Context, from p2pcrypto.PublicKey, message FirstVotingMessage) error {
	currentEpoch := tb.currentEpoch()

	currentEpochProposal, err := tb.calcProposal(currentEpoch)
	if err != nil {
		return fmt.Errorf("calculate proposal: %w", err)
	}

	// Ensure that epoch is the same.
	ok := tb.vrfVerifier(from.Bytes(), currentEpochProposal, message.Signature)
	if !ok {
		tb.Log.With().Warning("Received malformed first voting message, bad signature",
			log.String("from", from.String()),
			log.Uint64("epoch_id", uint64(currentEpoch)),
			log.String("expected_message", util.Bytes2Hex(currentEpochProposal)),
			log.String("signature", util.Bytes2Hex(message.Signature)))

		return nil
	}

	tb.Log.With().Info("Received first round voting message, counting it",
		log.String("message", message.String()))

	thisRound := epochRoundPair{
		EpochID: currentEpoch,
		Round:   firstRound,
	}

	tb.votesMu.Lock()
	defer tb.votesMu.Unlock()

	if _, ok := tb.incomingVotes[thisRound]; !ok {
		tb.incomingVotes[thisRound] = make(votesPerPK)
	}

	if _, ok := tb.firstRoundIncomingVotes[currentEpoch]; !ok {
		tb.firstRoundIncomingVotes[currentEpoch] = make(firstRoundVotesPerPK)
	}

	if _, ok := tb.incomingVotes[thisRound][from]; ok {
		tb.Log.With().Warning("Received malformed message, already received a voting message for these PK and round",
			log.String("from", from.String()),
			log.Uint64("round_id", uint64(firstRound)))

		return nil
	}

	validVotesMap := make(hashSet)
	invalidVotesMap := make(hashSet)
	validVotesList := make([]proposal, 0)
	potentiallyValidVotesList := make([]proposal, 0)

	for _, vote := range message.ValidVotes {
		validVotesMap[util.Bytes2Hex(vote)] = struct{}{}
		validVotesList = append(validVotesList, util.Bytes2Hex(vote))
	}

	for _, vote := range message.PotentiallyValidVotes {
		invalidVotesMap[util.Bytes2Hex(vote)] = struct{}{}
		potentiallyValidVotesList = append(potentiallyValidVotesList, util.Bytes2Hex(vote))
	}

	tb.incomingVotes[thisRound][from] = votesSetPair{
		ValidVotes:   validVotesMap,
		InvalidVotes: invalidVotesMap,
	}

	tb.firstRoundIncomingVotes[currentEpoch][from] = firstRoundVotes{
		ValidVotes:            validVotesList,
		PotentiallyValidVotes: potentiallyValidVotesList,
	}

	return nil
}

func (tb *TortoiseBeacon) handleFollowingVotingMessage(ctx context.Context, from p2pcrypto.PublicKey, message FollowingVotingMessage) error {
	currentEpoch := tb.currentEpoch()
	messageRound := message.RoundID

	currentEpochProposal, err := tb.calcProposal(currentEpoch)
	if err != nil {
		return fmt.Errorf("calculate proposal: %w", err)
	}

	// Ensure that epoch is the same.
	ok := tb.vrfVerifier(from.Bytes(), currentEpochProposal, message.Signature)
	if !ok {
		tb.Log.With().Warning("Received malformed following voting message, bad signature",
			log.String("from", from.String()),
			log.Uint64("epoch_id", uint64(currentEpoch)),
			log.String("expected_message", util.Bytes2Hex(currentEpochProposal)),
			log.String("signature", util.Bytes2Hex(message.Signature)))

		return nil
	}

	tb.Log.With().Info("Received VotingMessage, counting it",
		log.Uint64("round", uint64(messageRound)),
		log.String("message", message.String()))

	thisRound := epochRoundPair{
		EpochID: currentEpoch,
		Round:   messageRound,
	}

	tb.votesMu.Lock()
	defer tb.votesMu.Unlock()

	if _, ok := tb.incomingVotes[thisRound]; !ok {
		tb.incomingVotes[thisRound] = make(votesPerPK)
	}

	if _, ok := tb.incomingVotes[thisRound][from]; ok {
		tb.Log.With().Warning("Received malformed message, already received a voting message for these PK and round",
			log.String("from", from.String()),
			log.Uint64("round_id", uint64(message.RoundID)))

		return nil
	}

	firstRoundIncomingVotes := tb.firstRoundIncomingVotes[currentEpoch][from]
	tb.incomingVotes[thisRound][from] = tb.decodeVotes(message.VotesBitVector, firstRoundIncomingVotes)

	return nil
}

func (tb *TortoiseBeacon) currentEpoch() types.EpochID {
	return tb.currentLayer().GetEpoch()
}

func (tb *TortoiseBeacon) currentLayer() types.LayerID {
	tb.layerMu.RLock()
	defer tb.layerMu.RUnlock()

	return tb.lastLayer
}
