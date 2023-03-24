package beacon

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type category uint8

const (
	valid            category = 1
	potentiallyValid category = 2
	invalid          category = 3
)

var (
	errVRFNotVerified     = errors.New("proposal failed vrf verification")
	errAlreadyProposed    = errors.New("already proposed")
	errAlreadyVoted       = errors.New("already voted")
	errMinerNotActive     = errors.New("miner ATX not found in previous epoch")
	errProtocolNotRunning = errors.New("beacon protocol not running")
	errEpochNotActive     = errors.New("epoch not active")
	errMalformedMessage   = errors.New("malformed msg")
	errUntimelyMessage    = errors.New("untimely msg")
)

// HandleWeakCoinProposal handles weakcoin proposal from gossip.
func (pd *ProtocolDriver) HandleWeakCoinProposal(ctx context.Context, peer p2p.Peer, msg []byte) pubsub.ValidationResult {
	if pd.isClosed() || !pd.isInProtocol() {
		return pubsub.ValidationIgnore
	}

	return pd.weakCoin.HandleProposal(ctx, peer, msg)
}

// HandleProposal handles beacon proposal from gossip.
func (pd *ProtocolDriver) HandleProposal(ctx context.Context, peer p2p.Peer, msg []byte) pubsub.ValidationResult {
	if pd.isClosed() {
		return pubsub.ValidationIgnore
	}

	receivedTime := time.Now()
	err := pd.handleProposal(ctx, peer, msg, receivedTime)
	switch {
	case err == nil:
		return pubsub.ValidationAccept
	case errors.Is(err, errMalformedMessage):
		return pubsub.ValidationReject
	default:
		return pubsub.ValidationIgnore
	}
}

func (pd *ProtocolDriver) handleProposal(ctx context.Context, peer p2p.Peer, msg []byte, receivedTime time.Time) error {
	logger := pd.logger.WithContext(ctx)

	var m ProposalMessage
	if err := codec.Decode(msg, &m); err != nil {
		logger.With().Warning("received malformed beacon proposal", log.Stringer("sender", peer), log.Err(err))
		return errMalformedMessage
	}

	if !pd.isProposalTimely(&m, receivedTime) {
		logger.With().Debug("proposal too early", m.EpochID, log.Time("received_at", receivedTime))
		return errUntimelyMessage
	}

	logger = pd.logger.WithContext(ctx).WithFields(m.EpochID, log.Stringer("smesher", m.NodeID))
	proposal := cropData(m.VRFSignature)
	logger.With().Debug("new beacon proposal", log.String("proposal", hex.EncodeToString(proposal[:])))

	st, err := pd.initEpochStateIfNotPresent(logger, m.EpochID)
	if err != nil {
		return err
	}

	atx, err := pd.minerAtxHdr(m.EpochID, m.NodeID)
	if err != nil {
		return err
	}

	if err = pd.verifyProposalMessage(logger, m); err != nil {
		return err
	}

	cat := pd.classifyProposal(logger, m, atx.Received, receivedTime, st.proposalChecker)
	return pd.addProposal(m, cat)
}

func (pd *ProtocolDriver) classifyProposal(
	logger log.Log,
	m ProposalMessage,
	atxReceived, receivedTime time.Time,
	checker eligibilityChecker,
) category {
	epochStart := pd.clock.LayerToTime(m.EpochID.FirstLayer())
	proposal := cropData(m.VRFSignature)
	logger = logger.WithFields(
		log.String("proposal", hex.EncodeToString(proposal[:])),
		log.Time("atx_timestamp", atxReceived),
		log.Stringer("next_epoch_start", epochStart),
		log.Time("received_time", receivedTime),
		log.Duration("grace_period", pd.config.GracePeriodDuration),
	)

	// partition the proposals into three sets:
	//	let W1 be the weight at δ before the end of the previous epoch, with eligibility threshold X1
	//	let W2 be the weight at the end of the previous epoch, with eligibility threshold X2
	//  - valid:
	//	  * the proposer's ATX was received before the end of the previous epoch
	//	  * the proposal is received before the proposal phase ends.
	//	  * the proposal is lower than the threshold X2
	//	- potentially valid:
	//	  * the proposer's ATX was received within δ after the end of the previous epoch.
	//	  * the proposal is received within δ after the proposal phase ends.
	//	  * the proposal is lower than the threshold X1
	//  - invalid
	//    the proposal is neither valid nor potentially valid
	//
	// note that honest users cannot disagree on timing by more than δ, so if a proposal is timely for
	// any honest user, it cannot be late for any honest user (and vice versa).

	var (
		atxDelay      = atxReceived.Sub(epochStart)
		endTime       = pd.getProposalPhaseFinishedTime(m.EpochID)
		proposalDelay time.Duration
	)
	if !endTime.IsZero() {
		proposalDelay = receivedTime.Sub(endTime)
	}

	switch {
	case atxDelay <= 0 &&
		proposalDelay <= 0 &&
		checker.PassStrictThreshold(m.VRFSignature):
		logger.With().Debug("valid beacon proposal",
			log.Duration("atx delay", atxDelay),
			log.Duration("proposal delay", proposalDelay),
			log.String("proposal", hex.EncodeToString(proposal[:])),
		)
		return valid
	case atxDelay <= pd.config.GracePeriodDuration &&
		proposalDelay <= pd.config.GracePeriodDuration &&
		checker.PassThreshold(m.VRFSignature):
		logger.With().Debug("potentially valid beacon proposal",
			log.Duration("atx delay", atxDelay),
			log.Duration("proposal delay", proposalDelay),
			log.String("proposal", hex.EncodeToString(proposal[:])),
		)
		return potentiallyValid
	default:
		if atxDelay > pd.config.GracePeriodDuration || proposalDelay > pd.config.GracePeriodDuration {
			logger.With().Warning("invalid beacon proposal",
				log.Duration("atx delay", atxDelay),
				log.Duration("proposal delay", proposalDelay),
				log.String("proposal", hex.EncodeToString(proposal[:])),
			)
		} else {
			logger.With().Debug("proposal did not pass thresholds",
				log.Duration("atx delay", atxDelay),
				log.Duration("proposal delay", proposalDelay),
				log.String("proposal", hex.EncodeToString(proposal[:])),
			)
		}
	}
	return invalid
}

func cropData(data types.VrfSignature) Proposal {
	var shortened Proposal
	copy(shortened[:], data[:])
	return shortened
}

func (pd *ProtocolDriver) addProposal(m ProposalMessage, cat category) error {
	p := cropData(m.VRFSignature)
	pd.mu.Lock()
	defer pd.mu.Unlock()
	if _, ok := pd.states[m.EpochID]; !ok {
		return errEpochNotActive
	}
	switch cat {
	case valid:
		pd.states[m.EpochID].addValidProposal(p)
	case potentiallyValid:
		pd.states[m.EpochID].addPotentiallyValidProposal(p)
	}
	return nil
}

func (pd *ProtocolDriver) verifyProposalMessage(logger log.Log, m ProposalMessage) error {
	minerID := m.NodeID.String()

	nonce, err := pd.nonceFetcher.VRFNonce(m.NodeID, m.EpochID)
	if err != nil {
		logger.With().Warning("[proposal] failed to get VRF nonce", log.Err(err))
		return fmt.Errorf("[proposal] get VRF nonce (miner ID %v): %w", minerID, err)
	}
	currentEpochProposal := buildProposal(logger, m.EpochID, nonce)
	if !pd.vrfVerifier.Verify(m.NodeID, currentEpochProposal, m.VRFSignature) {
		// TODO(nkryuchkov): attach telemetry
		logger.With().Warning("[proposal] failed to verify VRF signature")
		return fmt.Errorf("[proposal] verify VRF (miner ID %v): %w", minerID, errVRFNotVerified)
	}

	vrfPK := signing.NewPublicKey(m.NodeID.Bytes())
	if err = pd.registerProposed(logger, m.EpochID, vrfPK); err != nil {
		logger.With().Warning("[proposal] failed to register miner proposed", log.Err(err))
		return fmt.Errorf("[proposal] register proposal (miner ID %v): %w", minerID, err)
	}

	return nil
}

// HandleFirstVotes handles beacon first votes from gossip.
func (pd *ProtocolDriver) HandleFirstVotes(ctx context.Context, peer p2p.Peer, msg []byte) pubsub.ValidationResult {
	if pd.isClosed() || !pd.isInProtocol() {
		pd.logger.WithContext(ctx).Debug("beacon protocol shutting down or not running, dropping msg")
		return pubsub.ValidationIgnore
	}

	logger := pd.logger.WithContext(ctx).WithFields(log.Stringer("sender", peer))
	logger.Debug("new first votes")
	err := pd.handleFirstVotes(ctx, peer, msg)
	switch {
	case err == nil:
		return pubsub.ValidationAccept
	case errors.Is(err, errMalformedMessage):
		return pubsub.ValidationReject
	default:
		return pubsub.ValidationIgnore
	}
}

func (pd *ProtocolDriver) handleFirstVotes(ctx context.Context, peer p2p.Peer, msg []byte) error {
	logger := pd.logger.WithContext(ctx).WithFields(types.FirstRound, log.Stringer("sender", peer))

	var m FirstVotingMessage
	if err := codec.Decode(msg, &m); err != nil {
		logger.With().Warning("received invalid first votes", log.Err(err))
		return errMalformedMessage
	}

	currentEpoch := pd.currentEpoch()
	if m.EpochID != currentEpoch {
		logger.With().Debug("first votes from different epoch",
			log.Uint32("current_epoch", uint32(currentEpoch)),
			log.Uint32("message_epoch", uint32(m.EpochID)))
		return errEpochNotActive
	}

	// don't accept more first vote after the round ends
	currentRound := pd.currentRound()
	if currentRound > types.FirstRound {
		logger.With().Debug("first votes too late",
			log.Uint32("current_round", uint32(currentRound)),
			log.Uint32("message_round", uint32(types.FirstRound)))
		return errUntimelyMessage
	}

	minerPK, err := pd.verifyFirstVotes(ctx, m)
	if err != nil {
		return err
	}

	logger.Debug("received first voting message, storing its votes")
	return pd.storeFirstVotes(m, minerPK)
}

func (pd *ProtocolDriver) verifyFirstVotes(ctx context.Context, m FirstVotingMessage) (types.NodeID, error) {
	logger := pd.logger.WithContext(ctx).WithFields(m.EpochID, types.FirstRound)
	messageBytes, err := codec.Encode(&m.FirstVotingMessageBody)
	if err != nil {
		logger.With().Fatal("failed to serialize first voting message", log.Err(err))
	}

	nodeID, err := pd.pubKeyExtractor.ExtractNodeID(signing.BEACON, messageBytes, m.Signature)
	if err != nil {
		return types.EmptyNodeID, fmt.Errorf("[round %v] recover ID %x: %w", types.FirstRound, m.Signature, err)
	}

	logger = logger.WithFields(log.Stringer("smesher", nodeID))

	if err = pd.registerVoted(logger, m.EpochID, nodeID, types.FirstRound); err != nil {
		return types.EmptyNodeID, fmt.Errorf("[round %v] register proposal (miner ID %v): %w", types.FirstRound, nodeID.String(), err)
	}
	return nodeID, nil
}

func (pd *ProtocolDriver) storeFirstVotes(m FirstVotingMessage, minerPK types.NodeID) error {
	if !pd.isInProtocol() {
		pd.logger.Debug("beacon not in protocol, not storing first votes")
		return errProtocolNotRunning
	}

	atx, err := pd.minerAtxHdr(m.EpochID, minerPK)
	if err != nil {
		return err
	}
	voteWeight := new(big.Int).SetUint64(atx.GetWeight())

	pd.mu.Lock()
	defer pd.mu.Unlock()

	if _, ok := pd.states[m.EpochID]; !ok {
		return errEpochNotActive
	}

	for _, proposal := range m.ValidProposals {
		pd.states[m.EpochID].addVote(proposal, up, voteWeight)
	}

	for _, proposal := range m.PotentiallyValidProposals {
		pd.states[m.EpochID].addVote(proposal, down, voteWeight)
	}

	// this is used for bit vector calculation
	voteList := append(m.ValidProposals, m.PotentiallyValidProposals...)
	if uint32(len(voteList)) > pd.config.VotesLimit {
		voteList = voteList[:pd.config.VotesLimit]
	}

	pd.states[m.EpochID].setMinerFirstRoundVote(minerPK, voteList)
	return nil
}

// HandleFollowingVotes handles beacon following votes from gossip.
func (pd *ProtocolDriver) HandleFollowingVotes(ctx context.Context, peer p2p.Peer, msg []byte) pubsub.ValidationResult {
	receivedTime := time.Now()

	if pd.isClosed() || !pd.isInProtocol() {
		pd.logger.WithContext(ctx).Debug("beacon protocol shutting down or not running, dropping msg")
		return pubsub.ValidationIgnore
	}

	logger := pd.logger.WithContext(ctx).WithFields(log.String("sender", peer.String()))
	logger.Debug("new following votes")
	err := pd.handleFollowingVotes(ctx, peer, msg, receivedTime)
	switch {
	case err == nil:
		return pubsub.ValidationAccept
	case errors.Is(err, errMalformedMessage):
		return pubsub.ValidationReject
	default:
		return pubsub.ValidationIgnore
	}
}

func (pd *ProtocolDriver) handleFollowingVotes(ctx context.Context, peer p2p.Peer, msg []byte, receivedTime time.Time) error {
	logger := pd.logger.WithContext(ctx).WithFields(log.String("sender", peer.String()))

	var m FollowingVotingMessage
	if err := codec.Decode(msg, &m); err != nil {
		logger.With().Warning("received malformed following votes", log.Err(err))
		return errMalformedMessage
	}

	currentEpoch := pd.currentEpoch()
	if m.EpochID != currentEpoch {
		logger.With().Debug("following votes from different epoch",
			log.Uint32("current_epoch", uint32(currentEpoch)),
			log.Uint32("message_epoch", uint32(m.EpochID)))
		return errEpochNotActive
	}

	// don't accept votes from future rounds
	if !pd.isVoteTimely(&m, receivedTime) {
		logger.With().Debug("following votes too early", m.RoundID, log.Time("received_at", receivedTime))
		return errUntimelyMessage
	}

	minerPK, err := pd.verifyFollowingVotes(ctx, m)
	if err != nil {
		return err
	}

	logger.Debug("received following voting message, counting its votes")
	if err = pd.storeFollowingVotes(m, minerPK); err != nil {
		logger.With().Warning("failed to store following votes", log.Err(err))
		return err
	}

	return nil
}

func (pd *ProtocolDriver) verifyFollowingVotes(ctx context.Context, m FollowingVotingMessage) (types.NodeID, error) {
	round := m.RoundID
	messageBytes, err := codec.Encode(&m.FollowingVotingMessageBody)
	if err != nil {
		pd.logger.With().Fatal("failed to serialize voting message", log.Err(err))
	}

	nodeID, err := pd.pubKeyExtractor.ExtractNodeID(signing.BEACON, messageBytes, m.Signature)
	if err != nil {
		return types.EmptyNodeID, fmt.Errorf("[round %v] recover ID from signature %x: %w", round, m.Signature, err)
	}

	logger := pd.logger.WithContext(ctx).WithFields(m.EpochID, round, log.Stringer("smesher", nodeID))

	if err := pd.registerVoted(logger, m.EpochID, nodeID, m.RoundID); err != nil {
		return types.EmptyNodeID, err
	}

	return nodeID, nil
}

func (pd *ProtocolDriver) storeFollowingVotes(m FollowingVotingMessage, nodeID types.NodeID) error {
	if !pd.isInProtocol() {
		pd.logger.Debug("beacon not in protocol, not storing following votes")
		return errProtocolNotRunning
	}

	atx, err := pd.minerAtxHdr(m.EpochID, nodeID)
	if err != nil {
		return err
	}
	voteWeight := new(big.Int).SetUint64(atx.GetWeight())

	firstRoundVotes, err := pd.getFirstRoundVote(m.EpochID, nodeID)
	if err != nil {
		return fmt.Errorf("get miner first round votes %v: %w", nodeID.String(), err)
	}

	thisRoundVotes := decodeVotes(m.VotesBitVector, firstRoundVotes)
	return pd.addToVoteMargin(m.EpochID, thisRoundVotes, voteWeight)
}

func (pd *ProtocolDriver) getProposalPhaseFinishedTime(epoch types.EpochID) time.Time {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	if s, ok := pd.states[epoch]; ok {
		return s.proposalPhaseFinishedTime
	}
	// if this epoch doesn't exist, it is finished. returns something
	// always in the past but different from time.Time{}
	return time.Time{}.Add(time.Second)
}

func (pd *ProtocolDriver) addToVoteMargin(epoch types.EpochID, thisRoundVotes allVotes, voteWeight *big.Int) error {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if _, ok := pd.states[epoch]; !ok {
		return errEpochNotActive
	}
	for proposal := range thisRoundVotes.support {
		pd.states[epoch].addVote(proposal, up, voteWeight)
	}

	for proposal := range thisRoundVotes.against {
		pd.states[epoch].addVote(proposal, down, voteWeight)
	}

	return nil
}

func (pd *ProtocolDriver) currentEpoch() types.EpochID {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	return pd.clock.CurrentLayer().GetEpoch()
}

func (pd *ProtocolDriver) currentRound() types.RoundID {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	return pd.roundInProgress
}

func (pd *ProtocolDriver) isProposalTimely(p *ProposalMessage, receivedTime time.Time) bool {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	currentEpoch := pd.clock.CurrentLayer().GetEpoch()
	switch p.EpochID {
	case currentEpoch:
		return true
	case currentEpoch + 1:
		return receivedTime.After(pd.earliestProposalTime)
	}
	return false
}

func (pd *ProtocolDriver) isVoteTimely(m *FollowingVotingMessage, receivedTime time.Time) bool {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	switch m.RoundID {
	case pd.roundInProgress:
		return true
	case pd.roundInProgress + 1:
		return receivedTime.After(pd.earliestVoteTime)
	}
	return false
}

func (pd *ProtocolDriver) registerProposed(logger log.Log, epoch types.EpochID, minerPK *signing.PublicKey) error {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	if _, ok := pd.states[epoch]; !ok {
		return errEpochNotActive
	}
	return pd.states[epoch].registerProposed(logger, minerPK)
}

func (pd *ProtocolDriver) registerVoted(logger log.Log, epoch types.EpochID, minerPK types.NodeID, round types.RoundID) error {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	if _, ok := pd.states[epoch]; !ok {
		return errEpochNotActive
	}
	return pd.states[epoch].registerVoted(logger, minerPK, round)
}
