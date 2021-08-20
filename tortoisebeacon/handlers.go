package tortoisebeacon

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
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
	// ErrMalformedSignature is returned when signature is malformed.
	ErrMalformedSignature = errors.New("malformed signature")

	// ErrAlreadyVoted is returned when miner has already voted.
	ErrAlreadyVoted = errors.New("already voted")
)

// HandleSerializedProposalMessage defines method to handle Tortoise Beacon proposal Messages from gossip.
func (tb *TortoiseBeacon) HandleSerializedProposalMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher) {
	if tb.IsClosed() {
		return
	}

	receivedTime := time.Now()

	tb.Log.With().Debug("New proposal message",
		log.String("sender", data.Sender().String()))

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

	atxID, err := tb.verifyProposalMessage(m, currentEpoch)
	if errors.Is(err, ErrProposalDoesntPassThreshold) || errors.Is(err, database.ErrNotFound) {
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
		return fmt.Errorf("failed to get ATXID %v header: %w", atxID, err)
	}

	atxTimestamp, err := tb.atxDB.GetAtxTimestamp(atxID)
	if err != nil {
		return fmt.Errorf("failed to get ATXID %v timestamp: %w", atxID, err)
	}

	atxEpoch := atxHeader.PubLayerID.GetEpoch()
	nextEpochStart := tb.clock.LayerToTime((atxEpoch + 1).FirstLayer())

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

		tb.incomingProposals.valid = append(tb.incomingProposals.valid, m.VRFSignature)

	case tb.isPotentiallyValidProposalMessage(currentEpoch, atxTimestamp, nextEpochStart, receivedTime):
		tb.Log.With().Debug("Received potentially valid proposal message",
			log.Uint32("epoch_id", uint32(currentEpoch)),
			log.String("message", m.String()),
			log.String("atx_timestamp", atxTimestamp.String()),
			log.String("next_epoch_start", nextEpochStart.String()),
			log.String("received_time", receivedTime.String()),
			log.Duration("grace_period", tb.config.GracePeriodDuration))

		tb.incomingProposals.potentiallyValid = append(tb.incomingProposals.potentiallyValid, m.VRFSignature)

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

func (tb *TortoiseBeacon) verifyProposalMessage(m ProposalMessage, currentEpoch types.EpochID) (types.ATXID, error) {
	currentEpochProposal, err := tb.buildProposal(currentEpoch)
	if err != nil {
		return types.ATXID{}, fmt.Errorf("calculate proposal: %w", err)
	}

	atxID, err := tb.atxDB.GetNodeAtxIDForEpoch(m.NodeID, currentEpoch-1)
	if errors.Is(err, database.ErrNotFound) {
		tb.Log.With().Warning("Miner has no ATXs in the previous epoch",
			log.String("miner_id", m.NodeID.ShortString()))

		return types.ATXID{}, database.ErrNotFound
	}

	if err != nil {
		return types.ATXID{}, fmt.Errorf("get node ATXID for epoch (miner ID %v): %w", m.NodeID.ShortString(), err)
	}

	vrfPK := signing.NewPublicKey(m.NodeID.VRFPublicKey)
	if !tb.vrfVerifier.Verify(vrfPK, currentEpochProposal, m.VRFSignature) {
		// TODO(nkryuchkov): attach telemetry
		tb.Log.With().Warning("Received malformed proposal message: VRF is not verified",
			log.String("sender", m.NodeID.ShortString()))

		// TODO(nkryuchkov): add a test for this case
		return types.ATXID{}, ErrMalformedProposal
	}

	epochWeight, _, err := tb.atxDB.GetEpochWeight(currentEpoch)
	if err != nil {
		return types.ATXID{}, fmt.Errorf("get epoch %v weight: %w", currentEpoch, err)
	}

	proposalShortString := types.BytesToHash(m.VRFSignature).ShortString()

	passes, err := tb.proposalPassesEligibilityThreshold(m.VRFSignature, epochWeight)
	if err != nil {
		// not a handling error
		tb.Log.With().Info("Miner's proposal doesn't pass threshold",
			log.String("miner_id", m.NodeID.ShortString()))

		return types.ATXID{}, fmt.Errorf("proposalPassesEligibilityThreshold: proposal=%v, weight=%v: %w",
			proposalShortString, epochWeight, err)
	}

	if !passes {
		tb.Log.With().Warning("Rejected proposal message which doesn't pass threshold",
			log.String("proposal", proposalShortString))

		return types.ATXID{}, ErrProposalDoesntPassThreshold
	}

	return atxID, nil
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

	tb.Log.With().Debug("New voting message",
		log.String("sender", data.Sender().String()))

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

	minerPK, atxID, err := tb.verifyFirstVotingMessage(message, currentEpoch)
	if errors.Is(err, database.ErrNotFound) || errors.Is(err, ErrMalformedSignature) || errors.Is(err, ErrAlreadyVoted) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("verify first voting message: %w", err)
	}

	atx, err := tb.atxDB.GetAtxHeader(atxID)
	if err != nil {
		return fmt.Errorf("atx header: %w", err)
	}

	voteWeight := new(big.Int).SetUint64(atx.GetWeight())

	tb.Log.With().Debug("Received first voting message, storing its votes",
		log.String("miner_id", minerPK.ShortString()),
		log.Uint32("epoch_id", uint32(currentEpoch)),
		log.Uint32("round_id", uint32(firstRound)))

	tb.storeFirstVotes(message, minerPK, voteWeight)

	return nil
}

func (tb *TortoiseBeacon) verifyFirstVotingMessage(message FirstVotingMessage, currentEpoch types.EpochID) (*signing.PublicKey, types.ATXID, error) {
	messageBytes, err := types.InterfaceToBytes(message.FirstVotingMessageBody)
	if err != nil {
		return nil, types.ATXID{}, fmt.Errorf("unmarshal first voting message: %w", err)
	}

	minerPK, err := tb.edVerifier.Extract(messageBytes, message.Signature)
	if err != nil {
		return nil, types.ATXID{}, fmt.Errorf("unable to recover ID from signature %x: %w", message.Signature, err)
	}

	// TODO(nkryuchkov): Ensure that epoch is the same.

	nodeID := types.NodeID{Key: minerPK.String()}
	atxID, err := tb.atxDB.GetNodeAtxIDForEpoch(nodeID, currentEpoch-1)
	if errors.Is(err, database.ErrNotFound) {
		tb.Log.With().Warning("Miner has no ATXs in the previous epoch",
			log.String("miner_id", minerPK.ShortString()))

		return nil, types.ATXID{}, database.ErrNotFound
	}

	if err != nil {
		return nil, types.ATXID{}, fmt.Errorf("get node ATXID for epoch (miner ID %v): %w", minerPK.ShortString(), err)
	}

	if !signing.Verify(minerPK, messageBytes, message.Signature) {
		tb.Log.With().Warning("Received malformed first voting message, bad signature",
			log.String("miner_id", minerPK.ShortString()),
			log.Uint32("epoch_id", uint32(currentEpoch)))

		return nil, types.ATXID{}, ErrMalformedSignature
	}

	tb.consensusMu.Lock()
	defer tb.consensusMu.Unlock()

	if tb.hasVoted[firstRound] == nil {
		tb.hasVoted[firstRound] = make(map[string]struct{})
	}

	// TODO(nkryuchkov): consider having a separate table for an epoch with one bit in it if atx/miner is voted already
	if _, ok := tb.hasVoted[firstRound][string(minerPK.Bytes())]; ok {
		tb.Log.With().Warning("Received malformed first voting message, already received a voting message for these PK and round",
			log.String("miner_id", minerPK.ShortString()),
			log.Uint32("epoch_id", uint32(currentEpoch)),
			log.Uint32("round_id", uint32(firstRound)))

		// TODO(nkryuchkov): report this miner through gossip
		// TODO(nkryuchkov): store evidence, generate malfeasance proof: union of two whole voting messages
		// TODO(nkryuchkov): handle malfeasance proof: we have a blacklist, on receiving, add to blacklist
		// TODO(nkryuchkov): blacklist format: key is epoch when blacklisting started, value is link to proof (union of messages)
		// TODO(nkryuchkov): ban id forever globally across packages since this epoch
		// TODO(nkryuchkov): (not tortoise beacon) do the same for ATXs

		return nil, types.ATXID{}, ErrAlreadyVoted
	}

	return minerPK, atxID, nil
}

func (tb *TortoiseBeacon) storeFirstVotes(message FirstVotingMessage, minerPK *signing.PublicKey, voteWeight *big.Int) {
	tb.consensusMu.Lock()
	defer tb.consensusMu.Unlock()

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

	tb.hasVoted[firstRound][string(minerPK.Bytes())] = struct{}{}

	// this is used for bit vector calculation
	// TODO(nkryuchkov): store sorted mixed valid+potentiallyValid
	tb.firstRoundIncomingVotes[string(minerPK.Bytes())] = proposals{
		valid:            message.ValidProposals,
		potentiallyValid: message.PotentiallyValidProposals,
	}
}

// HandleSerializedFollowingVotingMessage defines method to handle Tortoise Beacon following voting Messages from gossip.
func (tb *TortoiseBeacon) HandleSerializedFollowingVotingMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher) {
	if tb.IsClosed() {
		return
	}

	tb.Log.With().Debug("New voting message",
		log.String("sender", data.Sender().String()))

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
	currentEpoch := tb.currentEpoch()
	messageRound := message.RoundID

	minerPK, atxID, err := tb.verifyFollowingVotingMessage(message, currentEpoch)
	if errors.Is(err, database.ErrNotFound) || errors.Is(err, ErrMalformedSignature) || errors.Is(err, ErrAlreadyVoted) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("verify following voting message: %w", err)
	}

	atx, err := tb.atxDB.GetAtxHeader(atxID)
	if err != nil {
		return fmt.Errorf("atx header: %w", err)
	}

	voteWeight := new(big.Int).SetUint64(atx.GetWeight())

	tb.Log.With().Debug("Received following voting message, counting its votes",
		log.String("miner_id", minerPK.ShortString()),
		log.Uint32("epoch_id", uint32(currentEpoch)),
		log.Uint32("round_id", uint32(messageRound)))

	tb.storeFollowingVotes(message, minerPK, voteWeight)

	return nil
}

func (tb *TortoiseBeacon) verifyFollowingVotingMessage(message FollowingVotingMessage, currentEpoch types.EpochID) (*signing.PublicKey, types.ATXID, error) {
	messageBytes, err := types.InterfaceToBytes(message.FollowingVotingMessageBody)
	if err != nil {
		return nil, types.ATXID{}, fmt.Errorf("unmarshal first voting message: %w", err)
	}

	minerPK, err := tb.edVerifier.Extract(messageBytes, message.Signature)
	if err != nil {
		return nil, types.ATXID{}, fmt.Errorf("unable to recover ID from signature %x: %w", message.Signature, err)
	}

	nodeID := types.NodeID{Key: minerPK.String()}
	atxID, err := tb.atxDB.GetNodeAtxIDForEpoch(nodeID, currentEpoch-1)
	if errors.Is(err, database.ErrNotFound) {
		tb.Log.With().Warning("Miner has no ATXs in the previous epoch",
			log.String("miner_id", minerPK.ShortString()))

		return nil, types.ATXID{}, database.ErrNotFound
	}

	if err != nil {
		return nil, types.ATXID{}, fmt.Errorf("get node ATXID for epoch (miner ID %v): %w", minerPK.ShortString(), err)
	}

	if !signing.Verify(minerPK, messageBytes, message.Signature) {
		tb.Log.With().Warning("Received malformed following voting message, bad signature",
			log.String("miner_id", minerPK.ShortString()),
			log.Uint32("epoch_id", uint32(currentEpoch)))

		return nil, types.ATXID{}, ErrMalformedSignature
	}

	tb.consensusMu.Lock()
	defer tb.consensusMu.Unlock()

	if tb.hasVoted[message.RoundID-firstRound] == nil {
		tb.hasVoted[message.RoundID-firstRound] = make(map[string]struct{})
	}

	if _, ok := tb.hasVoted[message.RoundID-firstRound][string(minerPK.Bytes())]; ok {
		tb.Log.With().Warning("Received malformed following voting message, already received a voting message for these PK and round",
			log.String("miner_id", minerPK.ShortString()),
			log.Uint32("epoch_id", uint32(currentEpoch)),
			log.Uint32("round_id", uint32(message.RoundID-firstRound)))

		return nil, types.ATXID{}, ErrAlreadyVoted
	}

	return minerPK, atxID, nil
}

func (tb *TortoiseBeacon) storeFollowingVotes(message FollowingVotingMessage, minerPK *signing.PublicKey, voteWeight *big.Int) {
	tb.consensusMu.Lock()
	defer tb.consensusMu.Unlock()

	thisRoundVotes := tb.decodeVotes(message.VotesBitVector, tb.firstRoundIncomingVotes[string(minerPK.Bytes())])

	for vote := range thisRoundVotes.valid {
		if _, ok := tb.votesMargin[vote]; !ok {
			tb.votesMargin[vote] = new(big.Int).Set(voteWeight)
		} else {
			tb.votesMargin[vote].Add(tb.votesMargin[vote], voteWeight)
		}
	}

	for vote := range thisRoundVotes.invalid {
		if _, ok := tb.votesMargin[vote]; !ok {
			tb.votesMargin[vote] = new(big.Int).Neg(voteWeight)
		} else {
			tb.votesMargin[vote].Sub(tb.votesMargin[vote], voteWeight)
		}
	}

	tb.hasVoted[message.RoundID-firstRound][string(minerPK.Bytes())] = struct{}{}
}

func (tb *TortoiseBeacon) currentEpoch() types.EpochID {
	return tb.currentLayer().GetEpoch()
}

func (tb *TortoiseBeacon) currentLayer() types.LayerID {
	tb.layerMu.RLock()
	defer tb.layerMu.RUnlock()

	return tb.lastLayer
}
