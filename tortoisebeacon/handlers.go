package tortoisebeacon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// TBProposalProtocol is a protocol for sending Tortoise Beacon proposal messages through Gossip.
const TBProposalProtocol = "TBProposalGossip"

// TBFirstVotingProtocol is a protocol for sending Tortoise Beacon first voting messages through Gossip.
const TBFirstVotingProtocol = "TBFirstVotingGossip"

// TBFollowingVotingProtocol is a protocol for sending Tortoise Beacon following voting messages through Gossip.
const TBFollowingVotingProtocol = "TBFollowingVotingGossip"

var (
	// ErrMalformedProposal is returned if proposal message is malformed.
	ErrMalformedProposal = errors.New("malformed proposal message")
)

// HandleSerializedProposalMessage defines method to handle Tortoise Beacon proposal Messages from gossip.
func (tb *TortoiseBeacon) HandleSerializedProposalMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher) {
	tb.Log.With().Debug("New proposal message",
		log.String("from", data.Sender().String()))

	var m ProposalMessage
	if err := types.BytesToInterface(data.Bytes(), &m); err != nil {
		tb.Log.With().Error("Received malformed proposal message",
			log.String("message", string(data.Bytes())),
			log.Err(err))

		return
	}

	if err := tb.handleProposalMessage(ctx, m); err != nil {
		tb.Log.With().Error("Failed to handle proposal message",
			log.String("sender", data.Sender().String()),
			log.String("message", m.String()),
			log.Err(err))

		return
	}

	data.ReportValidation(ctx, TBProposalProtocol)
}

func (tb *TortoiseBeacon) handleProposalMessage(ctx context.Context, m ProposalMessage) error {
	receivedTimestamp := time.Now()
	currentEpoch := tb.currentEpoch()

	atxID, err := tb.atxDB.GetNodeAtxIDForEpoch(m.MinerID, currentEpoch-1)
	if err != nil {
		tb.Log.With().Warning("Miner has no ATXs in the previous epoch")
		return nil
	}

	currentEpochProposal, err := tb.buildProposal(currentEpoch)
	if err != nil {
		return fmt.Errorf("calculate proposal: %w", err)
	}

	sender := m.MinerID.VRFPublicKey // hash of a miner PK
	ok := tb.vrfVerifier(sender, currentEpochProposal, m.VRFSignature)
	if !ok {
		// TODO: attach telemetry
		tb.Log.With().Warning("Received malformed proposal message: VRF is not verified",
			log.String("sender", m.MinerID.String()),
			log.String("sender_short", m.MinerID.ShortString()))
		return nil
	}

	epochWeight, _, err := tb.atxDB.GetEpochWeight(currentEpoch)
	if err != nil {
		return fmt.Errorf("get epoch weight: %w", err)
	}

	passes, err := tb.proposalPassesEligibilityThreshold(m.VRFSignature, epochWeight)
	if err != nil {
		return fmt.Errorf("proposalPassesEligibilityThreshold: %w", err)
	}

	if !passes {
		tb.Log.With().Warning("Rejected proposal message which doesn't pass threshold")
		return nil
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
	case tb.isValidProposalMessage(atxTimestamp, nextEpochStart, receivedTimestamp):
		tb.Log.With().Debug("Received valid proposal message",
			log.Uint64("epoch_id", uint64(currentEpoch)),
			log.String("message", m.String()))

		tb.validProposalsMu.Lock()

		if _, ok := tb.validProposals[currentEpoch]; !ok {
			tb.validProposals[currentEpoch] = make(map[string]struct{})
		}

		tb.validProposals[currentEpoch][util.Bytes2Hex(m.VRFSignature)] = struct{}{}

		tb.validProposalsMu.Unlock()

	case tb.isPotentiallyValidProposalMessage(atxTimestamp, nextEpochStart, receivedTimestamp):
		tb.Log.With().Debug("Received potentially valid proposal message",
			log.Uint64("epoch_id", uint64(currentEpoch)),
			log.String("message", m.String()))

		tb.potentiallyValidProposalsMu.Lock()

		if _, ok := tb.potentiallyValidProposals[currentEpoch]; !ok {
			tb.potentiallyValidProposals[currentEpoch] = make(map[string]struct{})
		}

		tb.potentiallyValidProposals[currentEpoch][util.Bytes2Hex(m.VRFSignature)] = struct{}{}

		tb.potentiallyValidProposalsMu.Unlock()

	default:
		tb.Log.With().Warning("Received invalid proposal message",
			log.Uint64("epoch_id", uint64(currentEpoch)))
	}

	return nil
}

func (tb *TortoiseBeacon) isPotentiallyValidProposalMessage(atxTimestamp time.Time, nextEpochStart time.Time, receivedTimestamp time.Time) bool {
	delayedATX := atxTimestamp.Before(nextEpochStart.Add(tb.gracePeriodDuration))
	delayedProposal := tb.receivedBeforeProposalPhaseFinished(tb.currentEpoch(), receivedTimestamp.Add(-tb.gracePeriodDuration))

	return delayedATX && delayedProposal
}

func (tb *TortoiseBeacon) isValidProposalMessage(atxTimestamp time.Time, nextEpochStart time.Time, receivedTimestamp time.Time) bool {
	timelyATX := atxTimestamp.Before(nextEpochStart)
	timelyProposal := tb.receivedBeforeProposalPhaseFinished(tb.currentEpoch(), receivedTimestamp)

	return timelyATX && timelyProposal
}

// HandleSerializedFirstVotingMessage defines method to handle Tortoise Beacon first voting Messages from gossip.
func (tb *TortoiseBeacon) HandleSerializedFirstVotingMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher) {
	from := data.Sender()

	tb.Log.With().Debug("New voting message",
		log.String("from", from.String()))

	var m FirstVotingMessage
	if err := types.BytesToInterface(data.Bytes(), &m); err != nil {
		tb.Log.With().Error("Received invalid voting message",
			log.String("message", string(data.Bytes())),
			log.Err(err))

		return
	}

	if err := tb.handleFirstVotingMessage(ctx, m); err != nil {
		tb.Log.With().Error("Failed to handle voting message",
			log.Err(err))

		return
	}

	data.ReportValidation(ctx, TBFirstVotingProtocol)
}

// HandleSerializedFollowingVotingMessage defines method to handle Tortoise Beacon following voting Messages from gossip.
func (tb *TortoiseBeacon) HandleSerializedFollowingVotingMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher) {
	from := data.Sender()

	tb.Log.With().Debug("New voting message",
		log.String("from", from.String()))

	var m FollowingVotingMessage
	if err := types.BytesToInterface(data.Bytes(), &m); err != nil {
		tb.Log.With().Error("Received invalid voting message",
			log.String("message", string(data.Bytes())),
			log.Err(err))

		return
	}

	if err := tb.handleFollowingVotingMessage(ctx, m); err != nil {
		tb.Log.With().Error("Failed to handle voting message",
			log.Err(err))

		return
	}

	data.ReportValidation(ctx, TBFollowingVotingProtocol)
}

func (tb *TortoiseBeacon) handleFirstVotingMessage(ctx context.Context, message FirstVotingMessage) error {
	currentEpoch := tb.currentEpoch()
	from := message.MinerID

	_, err := tb.atxDB.GetNodeAtxIDForEpoch(from, currentEpoch-1)
	if err != nil {
		tb.Log.With().Warning("Miner has no ATXs in the previous epoch")
		return nil
	}

	ok, err := tb.verifyEligibilityProof(message.FirstVotingMessageBody, from, message.Signature)
	if err != nil {
		return err
	}
	if !ok {
		tb.Log.With().Warning("Received malformed first voting message, bad signature",
			log.String("from", from.Key),
			log.Uint64("epoch_id", uint64(currentEpoch)),
			log.String("signature", util.Bytes2Hex(message.Signature)))

		return nil
	}

	tb.Log.With().Debug("Received first round voting message, counting it",
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

	// TODO: no need to store each vote separately
	// have a separate table for an epoch with one bit in it if atx/miner is voted already
	if _, ok := tb.incomingVotes[thisRound][from.Key]; ok {
		tb.Log.With().Warning("Received malformed first voting message, already received a voting message for these PK and round",
			log.String("from", from.Key),
			log.Uint64("epoch_id", uint64(currentEpoch)),
			log.Uint64("round_id", uint64(firstRound)))

		// TODO: report this miner through gossip
		// TODO: store evidence, generate malfeasance proof: union of two whole voting messages
		// TODO: handle malfeasance proof: we have a blacklist, on receiving, add to blacklist
		// TODO: blacklist format: key is epoch when blacklisting started, value is link to proof (union of messages)
		// TODO: ban id forever globally across packages since this epoch
		// TODO: (not tortoise beacon) do the same for ATXs

		return nil
	}

	tb.Log.With().Debug("Received first voting message, counting it",
		log.String("from", from.Key),
		log.Uint64("epoch_id", uint64(currentEpoch)),
		log.Uint64("round_id", uint64(firstRound)),
		log.String("message", message.String()))

	validVotesMap := make(hashSet)
	invalidVotesMap := make(hashSet)
	validVotesList := make([]proposal, 0)
	potentiallyValidVotesList := make([]proposal, 0)

	for _, vote := range message.ValidProposals {
		validVotesMap[util.Bytes2Hex(vote)] = struct{}{}
		validVotesList = append(validVotesList, util.Bytes2Hex(vote))
	}

	for _, vote := range message.PotentiallyValidProposals {
		invalidVotesMap[util.Bytes2Hex(vote)] = struct{}{}
		potentiallyValidVotesList = append(potentiallyValidVotesList, util.Bytes2Hex(vote))
	}

	tb.incomingVotes[thisRound][from.Key] = votesSetPair{
		ValidVotes:   validVotesMap,
		InvalidVotes: invalidVotesMap,
	}

	// this is used for bit vector calculation
	// TODO: store sorted mixed valid+potentiallyValid
	//
	tb.firstRoundIncomingVotes[currentEpoch][from.Key] = firstRoundVotes{
		ValidVotes:            validVotesList,
		PotentiallyValidVotes: potentiallyValidVotesList,
	}

	return nil
}

func (tb *TortoiseBeacon) handleFollowingVotingMessage(ctx context.Context, message FollowingVotingMessage) error {
	//currentEpoch := tb.currentEpoch()
	currentEpoch := message.EpochID
	messageRound := message.RoundID
	from := message.MinerID

	_, err := tb.atxDB.GetNodeAtxIDForEpoch(from, currentEpoch-1)
	if err != nil {
		tb.Log.With().Warning("Miner has no ATXs in the previous epoch")
		return nil
	}

	// Ensure that epoch is the same.
	ok, err := tb.verifyEligibilityProof(message.FollowingVotingMessageBody, from, message.Signature)
	if err != nil {
		return err
	}
	if !ok {
		tb.Log.With().Warning("Received malformed following voting message, bad signature",
			log.String("from", from.Key),
			log.Uint64("epoch_id", uint64(currentEpoch)),
			//log.String("expected_message", util.Bytes2Hex(currentEpochProposal)),
			log.String("signature", util.Bytes2Hex(message.Signature)))

		return nil
	}

	thisRound := epochRoundPair{
		EpochID: currentEpoch,
		Round:   messageRound,
	}

	tb.votesMu.Lock()
	defer tb.votesMu.Unlock()

	if _, ok := tb.incomingVotes[thisRound]; !ok {
		tb.incomingVotes[thisRound] = make(votesPerPK)
	}

	if _, ok := tb.incomingVotes[thisRound][from.Key]; ok {
		tb.Log.With().Warning("Received malformed following voting message, already received a voting message for these PK and round",
			log.String("from", from.Key),
			log.Uint64("epoch_id", uint64(currentEpoch)),
			log.Uint64("round_id", uint64(messageRound)))

		return nil
	}

	tb.Log.With().Debug("Received following voting message, counting it",
		log.String("from", from.Key),
		log.Uint64("epoch_id", uint64(currentEpoch)),
		log.Uint64("round_id", uint64(messageRound)),
		log.String("message", message.String()))

	firstRoundIncomingVotes := tb.firstRoundIncomingVotes[currentEpoch][from.Key]
	tb.incomingVotes[thisRound][from.Key] = tb.decodeVotes(message.VotesBitVector, firstRoundIncomingVotes)

	return nil
}

func (tb *TortoiseBeacon) verifyEligibilityProof(message interface{}, from types.NodeID, signature []byte) (bool, error) {
	messageBytes, err := types.InterfaceToBytes(message)
	if err != nil {
		return false, err
	}

	// TODO: Ensure that epoch is the same.

	ok := signing.Verify(signing.NewPublicKey(util.Hex2Bytes(from.Key)), messageBytes, signature)
	return ok, nil
}

func (tb *TortoiseBeacon) currentEpoch() types.EpochID {
	return tb.currentLayer().GetEpoch()
}

func (tb *TortoiseBeacon) currentLayer() types.LayerID {
	tb.layerMu.RLock()
	defer tb.layerMu.RUnlock()

	return tb.lastLayer
}
