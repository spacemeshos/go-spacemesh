package tortoisebeacon

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
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

	// ErrProposalDoesntPassThreshold is returned if proposal message doesn't pass threshold.
	ErrProposalDoesntPassThreshold = errors.New("proposal doesn't pass threshold")
)

// HandleSerializedProposalMessage defines method to handle Tortoise Beacon proposal Messages from gossip.
func (tb *TortoiseBeacon) HandleSerializedProposalMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher) {
	if tb.IsClosed() {
		return
	}

	receivedTime := time.Now()

	tb.Log.With().Debug("New proposal message",
		log.String("from", data.Sender().String()))

	var message ProposalMessage
	if err := types.BytesToInterface(data.Bytes(), &message); err != nil {
		tb.Log.With().Error("Received malformed proposal message",
			log.Err(err))

		return
	}

	currentEpoch := tb.currentEpoch()
	if message.EpochID < currentEpoch {
		tb.Log.With().Debug("Received proposal message from previous epoch, ignoring",
			log.Uint64("message_epoch", uint64(message.EpochID)),
			log.Uint32("current_epoch", uint32(currentEpoch)))

		return
	}

	tb.proposalChansMu.Lock()
	ch := tb.getOrCreateProposalChannel(message.EpochID)
	tb.proposalChansMu.Unlock()

	proposalWithReceipt := &proposalMessageWithReceiptData{
		message:      message,
		gossip:       data,
		receivedTime: receivedTime,
	}

	select {
	case <-ctx.Done():
	case ch <- proposalWithReceipt:
	}
}

func (tb *TortoiseBeacon) handleProposalMessage(m ProposalMessage, receivedTime time.Time) error {
	currentEpoch := tb.currentEpoch()

	atxID, err := tb.atxDB.GetNodeAtxIDForEpoch(m.MinerID, currentEpoch-1)
	if errors.Is(err, database.ErrNotFound) {
		tb.Log.With().Warning("Miner has no ATXs in the previous epoch",
			log.String("miner_id", m.MinerID.Key))

		return nil
	}

	if err != nil {
		return fmt.Errorf("get node ATXID for epoch (miner ID %v): %w", m.MinerID.Key, err)
	}

	err = tb.verifyProposalMessage(m, currentEpoch)
	if errors.Is(err, ErrProposalDoesntPassThreshold) {
		// not a handling error
		tb.Log.With().Info("Miner's proposal doesn't pass threshold",
			log.String("miner_id", m.MinerID.Key))

		return nil
	}

	if err != nil {
		return err
	}

	if err := tb.classifyProposalMessage(m, atxID, currentEpoch, receivedTime); err != nil {
		return fmt.Errorf("classify proposal message: %w", err)
	}

	return nil
}

func (tb *TortoiseBeacon) classifyProposalMessage(m ProposalMessage, atxID types.ATXID, currentEpoch types.EpochID, receivedTime time.Time) error {
	atxHeader, err := tb.atxDB.GetAtxHeader(atxID)
	if err != nil {
		return fmt.Errorf("failed to get ATXID %v header (miner ID %v): %w", atxID, m.MinerID, err)
	}

	atxTimestamp, err := tb.atxDB.GetAtxTimestamp(atxID)
	if err != nil {
		return fmt.Errorf("failed to get ATXID %v timestamp (miner ID %v): %w", atxID, m.MinerID, err)
	}

	atxEpoch := atxHeader.PubLayerID.GetEpoch()
	nextEpochStart := tb.clock.LayerToTime((atxEpoch + 1).FirstLayer())

	signature := string(m.VRFSignature)

	// Each smesher partitions the valid proposals received in the previous epoch into three sets:
	// - Timely proposals: received up to δ after the end of the previous epoch.
	// - Delayed proposals: received between δ and 2δ after the end of the previous epoch.
	// - Late proposals: more than 2δ after the end of the previous epoch.
	// Note that honest users cannot disagree on timing by more than δ,
	// so if a proposal is timely for any honest user,
	// it cannot be late for any honest user (and vice versa).

	switch {
	case tb.isValidProposalMessage(currentEpoch, atxTimestamp, nextEpochStart, receivedTime):
		tb.Log.With().Debug("Received valid proposal message",
			log.Uint32("epoch_id", uint32(currentEpoch)),
			log.String("message", m.String()),
			log.String("atx_timestamp", atxTimestamp.String()),
			log.String("next_epoch_start", nextEpochStart.String()),
			log.String("received_time", receivedTime.String()),
			log.Duration("grace_period", tb.config.GracePeriodDuration))

		tb.incomingProposals.ValidProposals = append(tb.incomingProposals.ValidProposals, signature)

	case tb.isPotentiallyValidProposalMessage(currentEpoch, atxTimestamp, nextEpochStart, receivedTime):
		tb.Log.With().Debug("Received potentially valid proposal message",
			log.Uint32("epoch_id", uint32(currentEpoch)),
			log.String("message", m.String()),
			log.String("atx_timestamp", atxTimestamp.String()),
			log.String("next_epoch_start", nextEpochStart.String()),
			log.String("received_time", receivedTime.String()),
			log.Duration("grace_period", tb.config.GracePeriodDuration))

		tb.incomingProposals.PotentiallyValidProposals = append(tb.incomingProposals.PotentiallyValidProposals, signature)

	default:
		tb.Log.With().Warning("Received invalid proposal message",
			log.Uint32("epoch_id", uint32(currentEpoch)),
			log.String("atx_timestamp", atxTimestamp.String()),
			log.String("next_epoch_start", nextEpochStart.String()),
			log.String("received_time", receivedTime.String()),
			log.Duration("grace_period", tb.config.GracePeriodDuration))
	}

	return nil
}

func (tb *TortoiseBeacon) verifyProposalMessage(m ProposalMessage, currentEpoch types.EpochID) error {
	currentEpochProposal, err := tb.buildProposal(currentEpoch)
	if err != nil {
		return fmt.Errorf("calculate proposal: %w", err)
	}

	if !tb.vrfVerifier.Verify(signing.NewPublicKey(m.MinerID.VRFPublicKey), currentEpochProposal, m.VRFSignature) {
		// TODO(nkryuchkov): attach telemetry
		tb.Log.With().Warning("Received malformed proposal message: VRF is not verified",
			log.String("sender", m.MinerID.Key))

		// TODO(nkryuchkov): add a test for this case
		return ErrMalformedProposal
	}

	epochWeight, _, err := tb.atxDB.GetEpochWeight(currentEpoch)
	if err != nil {
		return fmt.Errorf("get epoch %v weight: %w", currentEpoch, err)
	}

	proposalShortString := types.BytesToHash(m.VRFSignature).ShortString()

	passes, err := tb.proposalPassesEligibilityThreshold(m.VRFSignature, epochWeight)
	if err != nil {
		return fmt.Errorf("proposalPassesEligibilityThreshold: proposal=%v, weight=%v: %w",
			proposalShortString, epochWeight, err)
	}

	if !passes {
		tb.Log.With().Warning("Rejected proposal message which doesn't pass threshold",
			log.String("proposal", proposalShortString))

		return ErrProposalDoesntPassThreshold
	}

	return nil
}

func (tb *TortoiseBeacon) isPotentiallyValidProposalMessage(currentEpoch types.EpochID, atxTimestamp, nextEpochStart, receivedTimestamp time.Time) bool {
	delayedATX := atxTimestamp.Before(nextEpochStart.Add(tb.config.GracePeriodDuration))
	delayedProposal := tb.receivedBeforeProposalPhaseFinished(currentEpoch, receivedTimestamp.Add(-tb.config.GracePeriodDuration))

	return delayedATX && delayedProposal
}

func (tb *TortoiseBeacon) isValidProposalMessage(currentEpoch types.EpochID, atxTimestamp, nextEpochStart, receivedTimestamp time.Time) bool {
	timelyATX := atxTimestamp.Before(nextEpochStart)
	timelyProposal := tb.receivedBeforeProposalPhaseFinished(currentEpoch, receivedTimestamp)

	return timelyATX && timelyProposal
}

// HandleSerializedFirstVotingMessage defines method to handle Tortoise Beacon first voting Messages from gossip.
func (tb *TortoiseBeacon) HandleSerializedFirstVotingMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher) {
	if tb.IsClosed() {
		return
	}

	minerID := data.Sender()

	tb.Log.With().Debug("New voting message",
		log.String("miner_id", minerID.String()))

	var m FirstVotingMessage
	if err := types.BytesToInterface(data.Bytes(), &m); err != nil {
		tb.Log.With().Error("Received invalid voting message",
			log.String("message", string(data.Bytes())),
			log.Err(err))

		return
	}

	if err := tb.handleFirstVotingMessage(m); err != nil {
		tb.Log.With().Error("Failed to handle first voting message",
			log.Err(err))

		return
	}

	data.ReportValidation(ctx, TBFirstVotingProtocol)
}

func (tb *TortoiseBeacon) handleFirstVotingMessage(message FirstVotingMessage) error {
	currentEpoch := tb.currentEpoch()
	minerID := message.MinerID

	_, err := tb.atxDB.GetNodeAtxIDForEpoch(minerID, currentEpoch-1)
	if errors.Is(err, database.ErrNotFound) {
		tb.Log.With().Warning("Miner has no ATXs in the previous epoch",
			log.String("miner_id", minerID.Key))

		return nil
	}

	if err != nil {
		return fmt.Errorf("get node ATXID for epoch (miner ID %v): %w", minerID.Key, err)
	}

	ok, err := tb.verifyEligibilityProof(message.FirstVotingMessageBody, minerID, message.Signature)
	if err != nil {
		return err
	}

	if !ok {
		tb.Log.With().Warning("Received malformed first voting message, bad signature",
			log.String("miner_id", minerID.Key),
			log.Uint32("epoch_id", uint32(currentEpoch)))

		return nil
	}

	voteWeight, err := tb.voteWeight(minerID.Key, currentEpoch)
	if err != nil {
		return fmt.Errorf("get vote weight for epoch %v (miner ID %v): %w", currentEpoch, minerID.Key, err)
	}

	tb.Log.With().Debug("Received first round voting message, counting it",
		log.String("message", message.String()))

	currentRound := firstRound

	tb.votesMu.Lock()
	defer tb.votesMu.Unlock()

	if tb.hasVoted[currentRound-firstRound] == nil {
		tb.hasVoted[currentRound-firstRound] = make(map[nodeID]struct{})
	}

	// TODO: no need to store each vote separately
	// have a separate table for an epoch with one bit in it if atx/miner is voted already
	if _, ok := tb.hasVoted[currentRound-firstRound][minerID.Key]; ok {
		tb.Log.With().Warning("Received malformed first voting message, already received a voting message for these PK and round",
			log.String("miner_id", minerID.Key),
			log.Uint32("epoch_id", uint32(currentEpoch)),
			log.Uint32("round_id", uint32(currentRound)))

		// TODO: report this miner through gossip
		// TODO: store evidence, generate malfeasance proof: union of two whole voting messages
		// TODO: handle malfeasance proof: we have a blacklist, on receiving, add to blacklist
		// TODO: blacklist format: key is epoch when blacklisting started, value is link to proof (union of messages)
		// TODO: ban id forever globally across packages since this epoch
		// TODO: (not tortoise beacon) do the same for ATXs

		return nil
	}

	tb.Log.With().Debug("Received first voting message, counting it",
		log.String("miner_id", minerID.Key),
		log.Uint32("epoch_id", uint32(currentEpoch)),
		log.Uint32("round_id", uint32(currentRound)))

	for _, vote := range message.ValidProposals {
		if _, ok := tb.votesMargin[string(vote)]; !ok {
			tb.votesMargin[string(vote)] = new(big.Int).Set(voteWeight)
		} else {
			tb.votesMargin[string(vote)].Add(tb.votesMargin[string(vote)], voteWeight)
		}
	}

	for _, vote := range message.PotentiallyValidProposals {
		if _, ok := tb.votesMargin[string(vote)]; !ok {
			tb.votesMargin[string(vote)] = new(big.Int).Neg(voteWeight)
		} else {
			tb.votesMargin[string(vote)].Sub(tb.votesMargin[string(vote)], voteWeight)
		}
	}

	tb.hasVoted[currentRound-firstRound][minerID.Key] = struct{}{}

	// this is used for bit vector calculation
	// TODO: store sorted mixed valid+potentiallyValid
	tb.firstRoundIncomingVotes[minerID.Key] = proposalsBytes{
		ValidProposals:            message.ValidProposals,
		PotentiallyValidProposals: message.PotentiallyValidProposals,
	}

	return nil
}

// HandleSerializedFollowingVotingMessage defines method to handle Tortoise Beacon following voting Messages from gossip.
func (tb *TortoiseBeacon) HandleSerializedFollowingVotingMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher) {
	if tb.IsClosed() {
		return
	}

	minerID := data.Sender()

	tb.Log.With().Debug("New voting message",
		log.String("miner_id", minerID.String()))

	var m FollowingVotingMessage
	if err := types.BytesToInterface(data.Bytes(), &m); err != nil {
		tb.Log.With().Error("Received invalid voting message",
			log.String("message", string(data.Bytes())),
			log.Err(err))

		return
	}

	if err := tb.handleFollowingVotingMessage(m); err != nil {
		tb.Log.With().Error("Failed to handle following voting message",
			log.Err(err))

		return
	}

	data.ReportValidation(ctx, TBFollowingVotingProtocol)
}

func (tb *TortoiseBeacon) handleFollowingVotingMessage(message FollowingVotingMessage) error {
	// TODO(nkryuchkov): use tb.currentEpoch()
	// currentEpoch := tb.currentEpoch()
	currentEpoch := message.EpochID
	messageRound := message.RoundID
	minerID := message.MinerID

	_, err := tb.atxDB.GetNodeAtxIDForEpoch(minerID, currentEpoch-1)
	if errors.Is(err, database.ErrNotFound) {
		tb.Log.With().Warning("Miner has no ATXs in the previous epoch",
			log.String("miner_id", minerID.Key))

		return nil
	}

	if err != nil {
		return fmt.Errorf("get node ATXID for epoch (miner ID %v): %w", minerID.Key, err)
	}

	// Ensure that epoch is the same.
	ok, err := tb.verifyEligibilityProof(message.FollowingVotingMessageBody, minerID, message.Signature)
	if err != nil {
		return err
	}

	if !ok {
		tb.Log.With().Warning("Received malformed following voting message, bad signature",
			log.String("miner_id", minerID.Key),
			log.Uint32("epoch_id", uint32(currentEpoch)))

		return nil
	}

	voteWeight, err := tb.voteWeight(minerID.Key, currentEpoch)
	if err != nil {
		return fmt.Errorf("get vote weight for epoch %v (miner ID %v): %w", currentEpoch, minerID.Key, err)
	}

	tb.votesMu.Lock()
	defer tb.votesMu.Unlock()

	if tb.hasVoted[messageRound-firstRound] == nil {
		tb.hasVoted[messageRound-firstRound] = make(map[nodeID]struct{})
	}

	if _, ok := tb.hasVoted[messageRound-firstRound][minerID.Key]; ok {
		tb.Log.With().Warning("Received malformed following voting message, already received a voting message for these PK and round",
			log.String("miner_id", minerID.Key),
			log.Uint32("epoch_id", uint32(currentEpoch)),
			log.Uint32("round_id", uint32(messageRound)))

		return nil
	}

	tb.Log.With().Debug("Received following voting message, counting it",
		log.String("miner_id", minerID.Key),
		log.Uint32("epoch_id", uint32(currentEpoch)),
		log.Uint32("round_id", uint32(messageRound)))

	thisRoundVotes := tb.decodeVotes(message.VotesBitVector, tb.firstRoundIncomingVotes[minerID.Key])

	for vote := range thisRoundVotes.ValidVotes {
		if _, ok := tb.votesMargin[vote]; !ok {
			tb.votesMargin[vote] = new(big.Int).Set(voteWeight)
		} else {
			tb.votesMargin[vote].Add(tb.votesMargin[vote], voteWeight)
		}
	}

	for vote := range thisRoundVotes.InvalidVotes {
		if _, ok := tb.votesMargin[vote]; !ok {
			tb.votesMargin[vote] = new(big.Int).Neg(voteWeight)
		} else {
			tb.votesMargin[vote].Sub(tb.votesMargin[vote], voteWeight)
		}
	}

	tb.hasVoted[messageRound-firstRound][minerID.Key] = struct{}{}

	return nil
}

func (tb *TortoiseBeacon) verifyEligibilityProof(message interface{}, from types.NodeID, signature []byte) (bool, error) {
	messageBytes, err := types.InterfaceToBytes(message)
	if err != nil {
		return false, fmt.Errorf("InterfaceToBytes: %w", err)
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
