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
	// errVRFNotVerified is returned if proposal message fails VRF verification.
	errVRFNotVerified = errors.New("proposal failed vrf verification")

	// errProposalDoesntPassThreshold is returned if proposal message doesn't pass threshold.
	errProposalDoesntPassThreshold = errors.New("proposal doesn't pass threshold")

	// errAlreadyProposed is returned when miner doubly proposed for the same epoch.
	errAlreadyProposed = errors.New("already proposed")

	// errAlreadyVoted is returned when miner doubly voted for the same epoch/round.
	errAlreadyVoted = errors.New("already voted")

	// errMinerATXNotFound is returned when miner has no ATXs in the previous epoch.
	errMinerATXNotFound = errors.New("miner ATX not found in previous epoch")

	// errProtocolNotRunning is returned when we are not in the middle of tortoise beacon protocol
	errProtocolNotRunning = errors.New("tortoise beacon protocol not running")
)

// HandleSerializedProposalMessage defines method to handle Tortoise Beacon proposal Messages from gossip.
func (tb *TortoiseBeacon) HandleSerializedProposalMessage(ctx context.Context, data service.GossipMessage, _ service.Fetcher) {
	if tb.isClosed() || !tb.isInProtocol() {
		tb.logger.WithContext(ctx).Debug("tortoise beacon shutting down or not in protocol, dropping msg")
		return
	}

	receivedTime := time.Now()
	logger := tb.logger.WithContext(ctx)

	logger.With().Debug("new proposal message", log.String("sender", data.Sender().String()))

	var message ProposalMessage
	if err := types.BytesToInterface(data.Bytes(), &message); err != nil {
		logger.With().Warning("received malformed proposal message", log.Err(err))
		return
	}

	ch := tb.getProposalChannel(ctx, message.EpochID)
	if ch == nil {
		return
	}

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

func (tb *TortoiseBeacon) readProposalMessagesLoop(ctx context.Context, ch chan *proposalMessageWithReceiptData) {
	for {
		select {
		case <-ctx.Done():
			return

		case em := <-ch:
			if em == nil || tb.isClosed() || !tb.isInProtocol() {
				return
			}

			if err := tb.handleProposalMessage(ctx, em.message, em.receivedTime); err != nil {
				tb.logger.WithContext(ctx).With().Error("failed to handle proposal message",
					log.String("sender", em.gossip.Sender().String()),
					log.String("message", em.message.String()),
					log.Err(err))

				return
			}

			em.gossip.ReportValidation(ctx, TBProposalProtocol)
		}
	}
}

func (tb *TortoiseBeacon) handleProposalMessage(ctx context.Context, m ProposalMessage, receivedTime time.Time) error {
	currentEpoch := tb.currentEpoch()

	atxID, err := tb.verifyProposalMessage(ctx, m, currentEpoch)
	if err != nil {
		return err
	}

	if err = tb.classifyProposalMessage(ctx, m, atxID, currentEpoch, receivedTime); err != nil {
		return err
	}

	return nil
}

func (tb *TortoiseBeacon) classifyProposalMessage(ctx context.Context, m ProposalMessage, atxID types.ATXID, currentEpoch types.EpochID, receivedTime time.Time) error {
	minerID := m.NodeID.ShortString()
	logger := tb.logger.WithContext(ctx).WithFields(currentEpoch, log.String("miner_id", minerID))

	atxHeader, err := tb.atxDB.GetAtxHeader(atxID)
	if err != nil {
		logger.Error("[proposal] failed to get ATX header", atxID, log.Err(err))
		return fmt.Errorf("[proposal] failed to get ATX header (miner ID %v, ATX ID %v): %w", minerID, atxID, err)
	}

	atxTimestamp, err := tb.atxDB.GetAtxTimestamp(atxID)
	if err != nil {
		logger.Error("[proposal] failed to get ATX timestamp", atxID, log.Err(err))
		return fmt.Errorf("[proposal] failed to get ATX timestamp (miner ID %v, ATX ID %v): %w", minerID, atxID, err)
	}

	atxEpoch := atxHeader.PubLayerID.GetEpoch()
	nextEpochStart := tb.clock.LayerToTime((atxEpoch + 1).FirstLayer())

	logger = logger.WithFields(
		log.String("message", m.String()),
		log.String("atx_timestamp", atxTimestamp.String()),
		log.String("next_epoch_start", nextEpochStart.String()),
		log.String("received_time", receivedTime.String()),
		log.Duration("grace_period", tb.config.GracePeriodDuration))

	// Each smesher partitions the valid proposals received in the previous epoch into three sets:
	// - Timely proposals: received up to δ after the end of the previous epoch.
	// - Delayed proposals: received between δ and 2δ after the end of the previous epoch.
	// - Late proposals: more than 2δ after the end of the previous epoch.
	// Note that honest users cannot disagree on timing by more than δ,
	// so if a proposal is timely for any honest user,
	// it cannot be late for any honest user (and vice versa).

	switch {
	case tb.isValidProposalMessage(currentEpoch, atxTimestamp, nextEpochStart, receivedTime):
		logger.Debug("received valid proposal message")
		tb.addValidProposal(m.VRFSignature)

	case tb.isPotentiallyValidProposalMessage(currentEpoch, atxTimestamp, nextEpochStart, receivedTime):
		logger.Debug("received potentially valid proposal message")
		tb.addPotentiallyValidProposal(m.VRFSignature)

	default:
		logger.Warning("received invalid proposal message")
	}

	return nil
}

func (tb *TortoiseBeacon) addValidProposal(proposal []byte) {
	if !tb.isInProtocol() {
		tb.logger.Debug("tortoise beacon not in protocol, not adding valid proposals")
		return
	}

	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.incomingProposals.valid = append(tb.incomingProposals.valid, proposal)
}

func (tb *TortoiseBeacon) addPotentiallyValidProposal(proposal []byte) {
	if !tb.isInProtocol() {
		tb.logger.Debug("tortoise beacon not in protocol, not adding potentially valid proposals")
		return
	}

	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.incomingProposals.potentiallyValid = append(tb.incomingProposals.potentiallyValid, proposal)
}

func (tb *TortoiseBeacon) verifyProposalMessage(ctx context.Context, m ProposalMessage, currentEpoch types.EpochID) (types.ATXID, error) {
	minerID := m.NodeID.ShortString()
	logger := tb.logger.WithContext(ctx).WithFields(currentEpoch, log.String("miner_id", minerID))

	atxID, err := tb.atxDB.GetNodeAtxIDForEpoch(m.NodeID, currentEpoch-1)
	if errors.Is(err, database.ErrNotFound) {
		logger.Warning("[proposal] miner has no ATX in previous epoch")
		return types.ATXID{}, fmt.Errorf("[proposal] miner has no ATX in previous epoch (miner ID %v): %w", minerID, errMinerATXNotFound)
	}

	if err != nil {
		logger.With().Warning("[proposal] failed to find ATX for miner", log.Err(err))
		return types.ATXID{}, fmt.Errorf("[proposal] failed to get ATX for epoch (miner ID %v): %w", minerID, err)
	}

	vrfPK := signing.NewPublicKey(m.NodeID.VRFPublicKey)
	currentEpochProposal := buildProposal(currentEpoch, logger)
	if !tb.vrfVerifier.Verify(vrfPK, currentEpochProposal, m.VRFSignature) {
		// TODO(nkryuchkov): attach telemetry
		logger.Warning("[proposal] failed to verify VRF signature")
		return types.ATXID{}, fmt.Errorf("[proposal] failed to verify VRF signature (miner ID %v): %w", minerID, errVRFNotVerified)
	}

	if err := tb.registerProposed(vrfPK, logger); err != nil {
		return types.ATXID{}, fmt.Errorf("[proposal] failed to register proposal (miner ID %v): %w", minerID, err)
	}

	passes := tb.proposalChecker.IsProposalEligible(m.VRFSignature)
	if !passes {
		// the peer may have different total weight from us so that it passes threshold for the peer
		// but does not pass here
		proposalShortString := types.BytesToHash(m.VRFSignature).ShortString()
		logger.With().Warning("rejected proposal that doesn't pass threshold",
			log.String("proposal", proposalShortString),
			log.Uint64("total_weight", tb.epochWeight))
		return types.ATXID{}, fmt.Errorf("[proposal] not eligible (miner ID %v): %w", minerID, errProposalDoesntPassThreshold)
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
func (tb *TortoiseBeacon) HandleSerializedFirstVotingMessage(ctx context.Context, data service.GossipMessage, _ service.Fetcher) {
	if tb.isClosed() || !tb.isInProtocol() {
		tb.logger.WithContext(ctx).Debug("tortoise beacon shutting down or not in protocol, dropping msg")
		return
	}

	logger := tb.logger.WithContext(ctx).WithFields(
		log.String("sender", data.Sender().String()),
		log.String("message", string(data.Bytes())))
	logger.Debug("new first voting message")

	var m FirstVotingMessage
	if err := types.BytesToInterface(data.Bytes(), &m); err != nil {
		logger.With().Warning("received invalid voting message", log.Err(err))
		return
	}

	currentEpoch := tb.currentEpoch()
	if m.EpochID != currentEpoch {
		logger.With().Debug("first voting message from different epoch",
			log.Uint32("current_epoch", uint32(currentEpoch)),
			log.Uint32("message_epoch", uint32(m.EpochID)))
		return
	}

	if err := tb.handleFirstVotingMessage(ctx, m); err != nil {
		logger.With().Error("failed to handle first voting message", log.Err(err))
		return
	}

	data.ReportValidation(ctx, TBFirstVotingProtocol)
}

func (tb *TortoiseBeacon) handleFirstVotingMessage(ctx context.Context, message FirstVotingMessage) error {
	currentEpoch := tb.currentEpoch()

	minerPK, atxID, err := tb.verifyFirstVotingMessage(ctx, message, currentEpoch)
	if err != nil {
		return err
	}

	minerID := types.NodeID{Key: minerPK.String()}.ShortString()
	logger := tb.logger.WithContext(ctx).WithFields(currentEpoch, types.FirstRound, log.String("miner_id", minerID))
	atx, err := tb.atxDB.GetAtxHeader(atxID)
	if err != nil {
		logger.With().Error("failed to get ATX header", atxID, log.Err(err))
		return fmt.Errorf("[round %v] failed to get ATX header (miner ID %v, ATX ID %v): %w", types.FirstRound, minerID, atxID, err)
	}

	voteWeight := new(big.Int).SetUint64(atx.GetWeight())

	logger.With().Debug("received first voting message, storing its votes")
	tb.storeFirstVotes(message, minerPK, voteWeight)

	return nil
}

func (tb *TortoiseBeacon) verifyFirstVotingMessage(ctx context.Context, message FirstVotingMessage, currentEpoch types.EpochID) (*signing.PublicKey, types.ATXID, error) {
	logger := tb.logger.WithContext(ctx).WithFields(currentEpoch, types.FirstRound)
	messageBytes, err := types.InterfaceToBytes(message.FirstVotingMessageBody)
	if err != nil {
		logger.With().Panic("failed to serialize first voting message", log.Err(err))
	}

	minerPK, err := tb.edVerifier.Extract(messageBytes, message.Signature)
	if err != nil {
		return nil, types.ATXID{}, fmt.Errorf("[round %v] unable to recover ID from signature %x: %w", types.FirstRound, message.Signature, err)
	}

	nodeID := types.NodeID{Key: minerPK.String()}
	minerID := nodeID.ShortString()
	logger = logger.WithFields(log.String("miner_id", minerID))

	if err := tb.registerVoted(minerPK, types.FirstRound, logger); err != nil {
		return nil, types.ATXID{}, fmt.Errorf("[round %v] failed to register proposal (miner ID %v): %w", types.FirstRound, minerID, err)
	}

	atxID, err := tb.atxDB.GetNodeAtxIDForEpoch(nodeID, currentEpoch-1)
	if errors.Is(err, database.ErrNotFound) {
		logger.Warning("miner has no ATX in the previous epoch")
		return nil, types.ATXID{}, fmt.Errorf("[round %v] miner has no ATX in previous epoch (miner ID %v): %w", types.FirstRound, minerID, errMinerATXNotFound)
	}

	if err != nil {
		logger.Error("failed to get ATX")
		return nil, types.ATXID{}, fmt.Errorf("[round %v] failed to get ATX for epoch (miner ID %v): %w", types.FirstRound, minerID, err)
	}

	return minerPK, atxID, nil
}

func (tb *TortoiseBeacon) storeFirstVotes(message FirstVotingMessage, minerPK *signing.PublicKey, voteWeight *big.Int) {
	if !tb.isInProtocol() {
		tb.logger.Debug("tortoise beacon not in protocol, not storing first votes")
		return
	}

	tb.mu.Lock()
	defer tb.mu.Unlock()

	for _, proposal := range message.ValidProposals {
		p := string(proposal)
		if _, ok := tb.votesMargin[p]; !ok {
			tb.votesMargin[p] = new(big.Int).Set(voteWeight)
		} else {
			tb.votesMargin[p].Add(tb.votesMargin[p], voteWeight)
		}
	}

	for _, proposal := range message.PotentiallyValidProposals {
		p := string(proposal)
		if _, ok := tb.votesMargin[p]; !ok {
			tb.votesMargin[p] = new(big.Int).Neg(voteWeight)
		} else {
			tb.votesMargin[p].Sub(tb.votesMargin[p], voteWeight)
		}
	}

	// this is used for bit vector calculation
	// TODO(nkryuchkov): store sorted mixed valid+potentiallyValid
	tb.firstRoundIncomingVotes[string(minerPK.Bytes())] = proposals{
		valid:            message.ValidProposals,
		potentiallyValid: message.PotentiallyValidProposals,
	}
}

// HandleSerializedFollowingVotingMessage defines method to handle Tortoise Beacon following voting Messages from gossip.
func (tb *TortoiseBeacon) HandleSerializedFollowingVotingMessage(ctx context.Context, data service.GossipMessage, _ service.Fetcher) {
	if tb.isClosed() || !tb.isInProtocol() {
		tb.logger.WithContext(ctx).Debug("tortoise beacon shutting down or not in protocol, dropping msg")
		return
	}

	logger := tb.logger.WithContext(ctx).WithFields(
		log.String("sender", data.Sender().String()),
		log.String("message", string(data.Bytes())))

	logger.Debug("new voting message")

	var m FollowingVotingMessage
	if err := types.BytesToInterface(data.Bytes(), &m); err != nil {
		logger.With().Warning("received invalid voting message", log.Err(err))
		return
	}

	currentEpoch := tb.currentEpoch()
	if m.EpochID != currentEpoch {
		logger.With().Debug("following voting message from different epoch",
			log.Uint32("current_epoch", uint32(currentEpoch)),
			log.Uint32("message_epoch", uint32(m.EpochID)))
		return
	}

	if err := tb.handleFollowingVotingMessage(ctx, m); err != nil {
		logger.With().Error("failed to handle following voting message", log.Err(err))
		return
	}

	data.ReportValidation(ctx, TBFollowingVotingProtocol)
}

func (tb *TortoiseBeacon) handleFollowingVotingMessage(ctx context.Context, message FollowingVotingMessage) error {
	currentEpoch := tb.currentEpoch()

	minerPK, atxID, err := tb.verifyFollowingVotingMessage(ctx, message, currentEpoch)
	if err != nil {
		return err
	}

	minerID := types.NodeID{Key: minerPK.String()}.ShortString()
	logger := tb.logger.WithContext(ctx).WithFields(currentEpoch, message.RoundID, log.String("miner_id", minerID))

	atx, err := tb.atxDB.GetAtxHeader(atxID)
	if err != nil {
		logger.With().Error("failed to get ATX header", atxID, log.Err(err))
		return fmt.Errorf("[round %v] failed to get ATX header (miner ID %v, ATX ID %v): %w", message.RoundID, minerID, atxID, err)
	}

	voteWeight := new(big.Int).SetUint64(atx.GetWeight())

	logger.Debug("received following voting message, counting its votes")
	tb.storeFollowingVotes(message, minerPK, voteWeight)

	return nil
}

func (tb *TortoiseBeacon) verifyFollowingVotingMessage(ctx context.Context, message FollowingVotingMessage, currentEpoch types.EpochID) (*signing.PublicKey, types.ATXID, error) {
	round := message.RoundID
	messageBytes, err := types.InterfaceToBytes(message.FollowingVotingMessageBody)
	if err != nil {
		tb.logger.With().Panic("failed to serialize voting message", log.Err(err))
	}

	minerPK, err := tb.edVerifier.Extract(messageBytes, message.Signature)
	if err != nil {
		return nil, types.ATXID{}, fmt.Errorf("[round %v] unable to recover ID from signature %x: %w", round, message.Signature, err)
	}

	nodeID := types.NodeID{Key: minerPK.String()}
	minerID := nodeID.ShortString()
	logger := tb.logger.WithContext(ctx).WithFields(currentEpoch, round, log.String("miner_id", minerID))

	if err := tb.registerVoted(minerPK, message.RoundID, logger); err != nil {
		return nil, types.ATXID{}, err
	}

	atxID, err := tb.atxDB.GetNodeAtxIDForEpoch(nodeID, currentEpoch-1)
	if errors.Is(err, database.ErrNotFound) {
		logger.Warning("miner has no ATX in the previous epoch")
		return nil, types.ATXID{}, fmt.Errorf("[round %v] miner has no ATX in previous epoch (miner ID %v): %w", round, minerID, errMinerATXNotFound)
	}

	if err != nil {
		logger.Error("failed to get ATX")
		return nil, types.ATXID{}, fmt.Errorf("[round %v] failed to get ATX for epoch (miner ID %v): %w", round, minerID, err)
	}

	return minerPK, atxID, nil
}

func (tb *TortoiseBeacon) storeFollowingVotes(message FollowingVotingMessage, minerPK *signing.PublicKey, voteWeight *big.Int) {
	if !tb.isInProtocol() {
		tb.logger.Debug("tortoise beacon not in protocol, not storing following votes")
		return
	}

	tb.mu.Lock()
	defer tb.mu.Unlock()

	thisRoundVotes := tb.decodeVotes(message.VotesBitVector, tb.firstRoundIncomingVotes[string(minerPK.Bytes())])

	for vote := range thisRoundVotes.valid {
		if _, ok := tb.votesMargin[vote]; !ok {
			tb.votesMargin[vote] = new(big.Int).Set(voteWeight)
		} else {
			tb.votesMargin[vote].Add(tb.votesMargin[vote], voteWeight)
		}
	}

	// TODO(kimmy): keep later rounds votes in a separate buffer so we don't count them prematurely
	// tho i am not sure whether counting votes for later rounds early is a security concern.
	for vote := range thisRoundVotes.invalid {
		if _, ok := tb.votesMargin[vote]; !ok {
			tb.votesMargin[vote] = new(big.Int).Neg(voteWeight)
		} else {
			tb.votesMargin[vote].Sub(tb.votesMargin[vote], voteWeight)
		}
	}
}

func (tb *TortoiseBeacon) currentEpoch() types.EpochID {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	return tb.epochInProgress
}

func (tb *TortoiseBeacon) registerProposed(minerPK *signing.PublicKey, logger log.Log) error {
	if !tb.isInProtocol() {
		tb.logger.Debug("tortoise beacon not in protocol, not registering proposal")
		return errProtocolNotRunning
	}

	tb.mu.Lock()
	defer tb.mu.Unlock()

	minerID := string(minerPK.Bytes())
	if _, ok := tb.hasProposed[minerID]; ok {
		// see TODOs for registerVoted()
		logger.Warning("already received proposal from miner")
		return fmt.Errorf("already made proposal (miner ID %v): %w", minerID, errAlreadyProposed)
	}

	tb.hasProposed[minerID] = struct{}{}
	return nil
}

func (tb *TortoiseBeacon) registerVoted(minerPK *signing.PublicKey, round types.RoundID, logger log.Log) error {
	if !tb.isInProtocol() {
		tb.logger.Debug("tortoise beacon not in protocol, not registering votes")
		return errProtocolNotRunning
	}

	tb.mu.Lock()
	defer tb.mu.Unlock()

	if tb.hasVoted[round] == nil {
		tb.hasVoted[round] = make(map[string]struct{})
	}

	minerID := string(minerPK.Bytes())
	// TODO(nkryuchkov): consider having a separate table for an epoch with one bit in it if atx/miner is voted already
	if _, ok := tb.hasVoted[round][minerID]; ok {
		logger.Warning("already received vote from miner for this round")

		// TODO(nkryuchkov): report this miner through gossip
		// TODO(nkryuchkov): store evidence, generate malfeasance proof: union of two whole voting messages
		// TODO(nkryuchkov): handle malfeasance proof: we have a blacklist, on receiving, add to blacklist
		// TODO(nkryuchkov): blacklist format: key is epoch when blacklisting started, value is link to proof (union of messages)
		// TODO(nkryuchkov): ban id forever globally across packages since this epoch
		// TODO(nkryuchkov): (not tortoise beacon) do the same for ATXs

		return fmt.Errorf("[round %v] already voted (miner ID %v): %w", round, minerID, errAlreadyVoted)
	}

	tb.hasVoted[round][minerID] = struct{}{}
	return nil
}
