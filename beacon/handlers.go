package beacon

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	// ProposalProtocol is the protocol for sending proposal messages through Gossip.
	ProposalProtocol = "BeaconProposal"

	// FirstVoteProtocol is the protocol for sending first vote messages through Gossip.
	FirstVoteProtocol = "BeaconFirstVote"

	// FollowingVotingProtocol is the protocol for sending following vote messages through Gossip.
	FollowingVotingProtocol = "BeaconFollowingVotes"
)

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

	// errProtocolNotRunning is returned when we are not in the middle of the beacon protocol.
	errProtocolNotRunning = errors.New("beacon protocol not running")
)

// HandleSerializedProposalMessage defines method to handle proposal Messages from gossip.
func (pd *ProtocolDriver) HandleSerializedProposalMessage(ctx context.Context, pid peer.ID, msg []byte) pubsub.ValidationResult {
	if pd.isClosed() || !pd.isInProtocol() {
		pd.logger.WithContext(ctx).Debug("beacon protocol shutting down or not running, dropping msg")
		return pubsub.ValidationIgnore
	}
	receivedTime := time.Now()
	logger := pd.logger.WithContext(ctx)

	logger.With().Debug("new proposal message", log.String("sender", pid.String()))

	var message ProposalMessage
	if err := types.BytesToInterface(msg, &message); err != nil {
		logger.With().Warning("received malformed proposal message", log.Err(err))
		return pubsub.ValidationIgnore
	}

	ch := pd.getProposalChannel(ctx, message.EpochID)
	if ch == nil {
		return pubsub.ValidationIgnore
	}
	proposalWithReceipt := &proposalMessageWithReceiptData{
		message:      message,
		receivedTime: receivedTime,
	}
	// FIXME(dshulyak) buffer messages without channel.
	// if message.EpochID == currentEpoch + 1 : buffer message in the slice. when epoch starts, check that slice
	// if message.EpochID == currentEpoch     : validate without passing to the channel
	select {
	case <-ctx.Done():
	case ch <- proposalWithReceipt:
	}
	return pubsub.ValidationAccept
}

func (pd *ProtocolDriver) readProposalMessagesLoop(ctx context.Context, ch chan *proposalMessageWithReceiptData) {
	for {
		select {
		case <-ctx.Done():
			return
		case em := <-ch:
			if em == nil || pd.isClosed() || !pd.isInProtocol() {
				return
			}

			if err := pd.handleProposalMessage(ctx, em.message, em.receivedTime); err != nil {
				pd.logger.WithContext(ctx).With().Error("failed to handle proposal message",
					log.String("message", em.message.String()),
					log.Err(err))

				return
			}
		}
	}
}

func (pd *ProtocolDriver) handleProposalMessage(ctx context.Context, m ProposalMessage, receivedTime time.Time) error {
	currentEpoch := pd.currentEpoch()

	atxID, err := pd.verifyProposalMessage(ctx, m, currentEpoch)
	if err != nil {
		return err
	}

	if err = pd.classifyProposalMessage(ctx, m, atxID, currentEpoch, receivedTime); err != nil {
		return err
	}

	return nil
}

func (pd *ProtocolDriver) classifyProposalMessage(ctx context.Context, m ProposalMessage, atxID types.ATXID, currentEpoch types.EpochID, receivedTime time.Time) error {
	minerID := m.NodeID.ShortString()
	logger := pd.logger.WithContext(ctx).WithFields(currentEpoch, log.String("miner_id", minerID))

	atxHeader, err := pd.atxDB.GetAtxHeader(atxID)
	if err != nil {
		logger.Error("[proposal] failed to get ATX header", atxID, log.Err(err))
		return fmt.Errorf("[proposal] failed to get ATX header (miner ID %v, ATX ID %v): %w", minerID, atxID, err)
	}

	atxTimestamp, err := pd.atxDB.GetAtxTimestamp(atxID)
	if err != nil {
		logger.Error("[proposal] failed to get ATX timestamp", atxID, log.Err(err))
		return fmt.Errorf("[proposal] failed to get ATX timestamp (miner ID %v, ATX ID %v): %w", minerID, atxID, err)
	}

	atxEpoch := atxHeader.PubLayerID.GetEpoch()
	nextEpochStart := pd.clock.LayerToTime((atxEpoch + 1).FirstLayer())

	logger = logger.WithFields(
		log.String("message", m.String()),
		log.String("atx_timestamp", atxTimestamp.String()),
		log.String("next_epoch_start", nextEpochStart.String()),
		log.String("received_time", receivedTime.String()),
		log.Duration("grace_period", pd.config.GracePeriodDuration))

	// Each smesher partitions the valid proposals received in the previous epoch into three sets:
	// - Timely proposals: received up to δ after the end of the previous epoch.
	// - Delayed proposals: received between δ and 2δ after the end of the previous epoch.
	// - Late proposals: more than 2δ after the end of the previous epoch.
	// Note that honest users cannot disagree on timing by more than δ,
	// so if a proposal is timely for any honest user,
	// it cannot be late for any honest user (and vice versa).

	switch {
	case pd.isValidProposalMessage(currentEpoch, atxTimestamp, nextEpochStart, receivedTime):
		logger.Debug("received valid proposal message")
		pd.addValidProposal(m.VRFSignature)
	case pd.isPotentiallyValidProposalMessage(currentEpoch, atxTimestamp, nextEpochStart, receivedTime):
		logger.Debug("received potentially valid proposal message")
		pd.addPotentiallyValidProposal(m.VRFSignature)
	default:
		logger.Warning("received invalid proposal message")
	}

	return nil
}

func (pd *ProtocolDriver) addValidProposal(proposal []byte) {
	if !pd.isInProtocol() {
		pd.logger.Debug("beacon not in protocol, not adding valid proposals")
		return
	}

	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.incomingProposals.valid = append(pd.incomingProposals.valid, proposal)
}

func (pd *ProtocolDriver) addPotentiallyValidProposal(proposal []byte) {
	if !pd.isInProtocol() {
		pd.logger.Debug("beacon not in protocol, not adding potentially valid proposals")
		return
	}

	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.incomingProposals.potentiallyValid = append(pd.incomingProposals.potentiallyValid, proposal)
}

func (pd *ProtocolDriver) verifyProposalMessage(ctx context.Context, m ProposalMessage, currentEpoch types.EpochID) (types.ATXID, error) {
	minerID := m.NodeID.ShortString()
	logger := pd.logger.WithContext(ctx).WithFields(currentEpoch, log.String("miner_id", minerID))

	atxID, err := pd.atxDB.GetNodeAtxIDForEpoch(m.NodeID, currentEpoch-1)
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
	if !pd.vrfVerifier.Verify(vrfPK, currentEpochProposal, m.VRFSignature) {
		// TODO(nkryuchkov): attach telemetry
		logger.Warning("[proposal] failed to verify VRF signature")
		return types.ATXID{}, fmt.Errorf("[proposal] failed to verify VRF signature (miner ID %v): %w", minerID, errVRFNotVerified)
	}

	if err := pd.registerProposed(vrfPK, logger); err != nil {
		return types.ATXID{}, fmt.Errorf("[proposal] failed to register proposal (miner ID %v): %w", minerID, err)
	}

	pd.mu.RLock()
	passes := pd.proposalChecker.IsProposalEligible(m.VRFSignature)
	pd.mu.RUnlock()
	if !passes {
		// the peer may have different total weight from us so that it passes threshold for the peer
		// but does not pass here
		proposalShortString := types.BytesToHash(m.VRFSignature).ShortString()
		logger.With().Warning("rejected proposal that doesn't pass threshold",
			log.String("proposal", proposalShortString),
			log.Uint64("total_weight", pd.epochWeight))
		return types.ATXID{}, fmt.Errorf("[proposal] not eligible (miner ID %v): %w", minerID, errProposalDoesntPassThreshold)
	}

	return atxID, nil
}

func (pd *ProtocolDriver) isPotentiallyValidProposalMessage(currentEpoch types.EpochID, atxTimestamp, nextEpochStart, receivedTimestamp time.Time) bool {
	delayedATX := atxTimestamp.Before(nextEpochStart.Add(pd.config.GracePeriodDuration))
	delayedProposal := pd.receivedBeforeProposalPhaseFinished(currentEpoch, receivedTimestamp.Add(-pd.config.GracePeriodDuration))

	return delayedATX && delayedProposal
}

func (pd *ProtocolDriver) isValidProposalMessage(currentEpoch types.EpochID, atxTimestamp, nextEpochStart, receivedTimestamp time.Time) bool {
	timelyATX := atxTimestamp.Before(nextEpochStart)
	timelyProposal := pd.receivedBeforeProposalPhaseFinished(currentEpoch, receivedTimestamp)

	return timelyATX && timelyProposal
}

// HandleSerializedFirstVotingMessage defines method to handle first voting messages from gossip.
func (pd *ProtocolDriver) HandleSerializedFirstVotingMessage(ctx context.Context, pid peer.ID, msg []byte) pubsub.ValidationResult {
	if pd.isClosed() || !pd.isInProtocol() {
		pd.logger.WithContext(ctx).Debug("beacon protocol shutting down or not running, dropping msg")
		return pubsub.ValidationIgnore
	}

	logger := pd.logger.WithContext(ctx).WithFields(
		log.String("sender", pid.String()),
		log.Binary("message", msg))
	logger.Debug("new first voting message")

	var m FirstVotingMessage
	if err := types.BytesToInterface(msg, &m); err != nil {
		logger.With().Warning("received invalid voting message", log.Err(err))
		return pubsub.ValidationIgnore
	}

	currentEpoch := pd.currentEpoch()
	if m.EpochID != currentEpoch {
		logger.With().Debug("first voting message from different epoch",
			log.Uint32("current_epoch", uint32(currentEpoch)),
			log.Uint32("message_epoch", uint32(m.EpochID)))
		return pubsub.ValidationIgnore
	}

	if err := pd.handleFirstVotingMessage(ctx, m); err != nil {
		logger.With().Warning("failed to handle first voting message", log.Err(err))
		return pubsub.ValidationIgnore
	}
	return pubsub.ValidationAccept
}

func (pd *ProtocolDriver) handleFirstVotingMessage(ctx context.Context, message FirstVotingMessage) error {
	currentEpoch := pd.currentEpoch()

	minerPK, atxID, err := pd.verifyFirstVotingMessage(ctx, message, currentEpoch)
	if err != nil {
		return err
	}

	minerID := types.NodeID{Key: minerPK.String()}.ShortString()
	logger := pd.logger.WithContext(ctx).WithFields(currentEpoch, types.FirstRound, log.String("miner_id", minerID))
	atx, err := pd.atxDB.GetAtxHeader(atxID)
	if err != nil {
		logger.With().Error("failed to get ATX header", atxID, log.Err(err))
		return fmt.Errorf("[round %v] failed to get ATX header (miner ID %v, ATX ID %v): %w", types.FirstRound, minerID, atxID, err)
	}

	voteWeight := new(big.Int).SetUint64(atx.GetWeight())

	logger.Debug("received first voting message, storing its votes")
	pd.storeFirstVotes(message, minerPK, voteWeight)

	return nil
}

func (pd *ProtocolDriver) verifyFirstVotingMessage(ctx context.Context, message FirstVotingMessage, currentEpoch types.EpochID) (*signing.PublicKey, types.ATXID, error) {
	logger := pd.logger.WithContext(ctx).WithFields(currentEpoch, types.FirstRound)
	messageBytes, err := types.InterfaceToBytes(message.FirstVotingMessageBody)
	if err != nil {
		logger.With().Panic("failed to serialize first voting message", log.Err(err))
	}

	minerPK, err := pd.edVerifier.Extract(messageBytes, message.Signature)
	if err != nil {
		return nil, types.ATXID{}, fmt.Errorf("[round %v] unable to recover ID from signature %x: %w", types.FirstRound, message.Signature, err)
	}

	nodeID := types.NodeID{Key: minerPK.String()}
	minerID := nodeID.ShortString()
	logger = logger.WithFields(log.String("miner_id", minerID))

	if err := pd.registerVoted(minerPK, types.FirstRound, logger); err != nil {
		return nil, types.ATXID{}, fmt.Errorf("[round %v] failed to register proposal (miner ID %v): %w", types.FirstRound, minerID, err)
	}

	atxID, err := pd.atxDB.GetNodeAtxIDForEpoch(nodeID, currentEpoch-1)
	if errors.Is(err, database.ErrNotFound) {
		logger.Warning("miner has no ATX in the previous epoch")
		return nil, types.ATXID{}, fmt.Errorf("[round %v] miner has no ATX in previous epoch (miner ID %v): %w", types.FirstRound, minerID, errMinerATXNotFound)
	}

	if err != nil {
		logger.With().Error("failed to get ATX", log.Err(err))
		return nil, types.ATXID{}, fmt.Errorf("[round %v] failed to get ATX for epoch (miner ID %v): %w", types.FirstRound, minerID, err)
	}

	return minerPK, atxID, nil
}

func (pd *ProtocolDriver) storeFirstVotes(message FirstVotingMessage, minerPK *signing.PublicKey, voteWeight *big.Int) {
	if !pd.isInProtocol() {
		pd.logger.Debug("beacon not in protocol, not storing first votes")
		return
	}

	pd.mu.Lock()
	defer pd.mu.Unlock()

	for _, proposal := range message.ValidProposals {
		p := string(proposal)
		if _, ok := pd.votesMargin[p]; !ok {
			pd.votesMargin[p] = new(big.Int).Set(voteWeight)
		} else {
			pd.votesMargin[p].Add(pd.votesMargin[p], voteWeight)
		}
	}

	for _, proposal := range message.PotentiallyValidProposals {
		p := string(proposal)
		if _, ok := pd.votesMargin[p]; !ok {
			pd.votesMargin[p] = new(big.Int).Neg(voteWeight)
		} else {
			pd.votesMargin[p].Sub(pd.votesMargin[p], voteWeight)
		}
	}

	// this is used for bit vector calculation
	voteList := append(message.ValidProposals, message.PotentiallyValidProposals...)
	if uint32(len(voteList)) > pd.config.VotesLimit {
		voteList = voteList[:pd.config.VotesLimit]
	}

	pd.firstRoundIncomingVotes[string(minerPK.Bytes())] = voteList
}

// HandleSerializedFollowingVotingMessage defines method to handle following voting Messages from gossip.
func (pd *ProtocolDriver) HandleSerializedFollowingVotingMessage(ctx context.Context, pid peer.ID, msg []byte) pubsub.ValidationResult {
	if pd.isClosed() || !pd.isInProtocol() {
		pd.logger.WithContext(ctx).Debug("beacon protocol shutting down or not running, dropping msg")
		return pubsub.ValidationIgnore
	}

	logger := pd.logger.WithContext(ctx).WithFields(
		log.String("sender", pid.String()),
		log.Binary("message", msg))

	logger.Debug("new voting message")

	var m FollowingVotingMessage
	if err := types.BytesToInterface(msg, &m); err != nil {
		logger.With().Warning("received invalid voting message", log.Err(err))
		return pubsub.ValidationIgnore
	}

	currentEpoch := pd.currentEpoch()
	if m.EpochID != currentEpoch {
		logger.With().Debug("following voting message from different epoch",
			log.Uint32("current_epoch", uint32(currentEpoch)),
			log.Uint32("message_epoch", uint32(m.EpochID)))
		return pubsub.ValidationIgnore
	}

	if err := pd.handleFollowingVotingMessage(ctx, m); err != nil {
		logger.With().Warning("failed to handle following voting message", log.Err(err))
		return pubsub.ValidationIgnore
	}
	return pubsub.ValidationAccept
}

func (pd *ProtocolDriver) handleFollowingVotingMessage(ctx context.Context, message FollowingVotingMessage) error {
	currentEpoch := pd.currentEpoch()

	minerPK, atxID, err := pd.verifyFollowingVotingMessage(ctx, message, currentEpoch)
	if err != nil {
		return err
	}

	minerID := types.NodeID{Key: minerPK.String()}.ShortString()
	logger := pd.logger.WithContext(ctx).WithFields(currentEpoch, message.RoundID, log.String("miner_id", minerID))

	atx, err := pd.atxDB.GetAtxHeader(atxID)
	if err != nil {
		logger.With().Error("failed to get ATX header", atxID, log.Err(err))
		return fmt.Errorf("[round %v] failed to get ATX header (miner ID %v, ATX ID %v): %w", message.RoundID, minerID, atxID, err)
	}

	voteWeight := new(big.Int).SetUint64(atx.GetWeight())

	logger.Debug("received following voting message, counting its votes")
	pd.storeFollowingVotes(message, minerPK, voteWeight)

	return nil
}

func (pd *ProtocolDriver) verifyFollowingVotingMessage(ctx context.Context, message FollowingVotingMessage, currentEpoch types.EpochID) (*signing.PublicKey, types.ATXID, error) {
	round := message.RoundID
	messageBytes, err := types.InterfaceToBytes(message.FollowingVotingMessageBody)
	if err != nil {
		pd.logger.With().Panic("failed to serialize voting message", log.Err(err))
	}

	minerPK, err := pd.edVerifier.Extract(messageBytes, message.Signature)
	if err != nil {
		return nil, types.ATXID{}, fmt.Errorf("[round %v] unable to recover ID from signature %x: %w", round, message.Signature, err)
	}

	nodeID := types.NodeID{Key: minerPK.String()}
	minerID := nodeID.ShortString()
	logger := pd.logger.WithContext(ctx).WithFields(currentEpoch, round, log.String("miner_id", minerID))

	if err := pd.registerVoted(minerPK, message.RoundID, logger); err != nil {
		return nil, types.ATXID{}, err
	}

	atxID, err := pd.atxDB.GetNodeAtxIDForEpoch(nodeID, currentEpoch-1)
	if errors.Is(err, database.ErrNotFound) {
		logger.Warning("miner has no ATX in the previous epoch")
		return nil, types.ATXID{}, fmt.Errorf("[round %v] miner has no ATX in previous epoch (miner ID %v): %w", round, minerID, errMinerATXNotFound)
	}

	if err != nil {
		logger.With().Error("failed to get ATX", log.Err(err))
		return nil, types.ATXID{}, fmt.Errorf("[round %v] failed to get ATX for epoch (miner ID %v): %w", round, minerID, err)
	}

	return minerPK, atxID, nil
}

func (pd *ProtocolDriver) storeFollowingVotes(message FollowingVotingMessage, minerPK *signing.PublicKey, voteWeight *big.Int) {
	if !pd.isInProtocol() {
		pd.logger.Debug("beacon not in protocol, not storing following votes")
		return
	}

	pd.mu.Lock()
	defer pd.mu.Unlock()

	thisRoundVotes := decodeVotes(message.VotesBitVector, pd.firstRoundIncomingVotes[string(minerPK.Bytes())])

	for vote := range thisRoundVotes.valid {
		if _, ok := pd.votesMargin[vote]; !ok {
			pd.votesMargin[vote] = new(big.Int).Set(voteWeight)
		} else {
			pd.votesMargin[vote].Add(pd.votesMargin[vote], voteWeight)
		}
	}

	// TODO: don't accept votes in future round
	// https://github.com/spacemeshos/go-spacemesh/issues/2794
	for vote := range thisRoundVotes.invalid {
		if _, ok := pd.votesMargin[vote]; !ok {
			pd.votesMargin[vote] = new(big.Int).Neg(voteWeight)
		} else {
			pd.votesMargin[vote].Sub(pd.votesMargin[vote], voteWeight)
		}
	}
}

func (pd *ProtocolDriver) currentEpoch() types.EpochID {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	return pd.epochInProgress
}

func (pd *ProtocolDriver) registerProposed(minerPK *signing.PublicKey, logger log.Log) error {
	if !pd.isInProtocol() {
		pd.logger.Debug("beacon not in protocol, not registering proposal")
		return errProtocolNotRunning
	}

	pd.mu.Lock()
	defer pd.mu.Unlock()

	minerID := string(minerPK.Bytes())
	if _, ok := pd.hasProposed[minerID]; ok {
		// see TODOs for registerVoted()
		logger.Warning("already received proposal from miner")
		return fmt.Errorf("already made proposal (miner ID %v): %w", minerPK.ShortString(), errAlreadyProposed)
	}

	pd.hasProposed[minerID] = struct{}{}
	return nil
}

func (pd *ProtocolDriver) registerVoted(minerPK *signing.PublicKey, round types.RoundID, logger log.Log) error {
	if !pd.isInProtocol() {
		pd.logger.Debug("beacon not in protocol, not registering votes")
		return errProtocolNotRunning
	}

	pd.mu.Lock()
	defer pd.mu.Unlock()

	if pd.hasVoted[round] == nil {
		pd.hasVoted[round] = make(map[string]struct{})
	}

	minerID := string(minerPK.Bytes())
	// TODO(nkryuchkov): consider having a separate table for an epoch with one bit in it if atx/miner is voted already
	if _, ok := pd.hasVoted[round][minerID]; ok {
		logger.Warning("already received vote from miner for this round")

		// TODO(nkryuchkov): report this miner through gossip
		// TODO(nkryuchkov): store evidence, generate malfeasance proof: union of two whole voting messages
		// TODO(nkryuchkov): handle malfeasance proof: we have a blacklist, on receiving, add to blacklist
		// TODO(nkryuchkov): blacklist format: key is epoch when blacklisting started, value is link to proof (union of messages)
		// TODO(nkryuchkov): ban id forever globally across packages since this epoch
		// TODO(nkryuchkov): (not specific to beacon) do the same for ATXs

		return fmt.Errorf("[round %v] already voted (miner ID %v): %w", round, minerPK.ShortString(), errAlreadyVoted)
	}

	pd.hasVoted[round][minerID] = struct{}{}
	return nil
}
