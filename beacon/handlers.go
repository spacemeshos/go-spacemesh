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
	"github.com/spacemeshos/go-spacemesh/sql"
)

type category uint8

const (
	valid            category = 1
	potentiallyValid category = 2
	invalid          category = 3
)

var (
	errVRFNotVerified      = errors.New("proposal failed vrf verification")
	errProposalNotEligible = errors.New("proposal not eligible")
	errAlreadyProposed     = errors.New("already proposed")
	errAlreadyVoted        = errors.New("already voted")
	errMinerATXNotFound    = errors.New("miner ATX not found in previous epoch")
	errProtocolNotRunning  = errors.New("beacon protocol not running")
	errEpochNotActive      = errors.New("epoch not active")
	errMalformedMessage    = errors.New("malformed msg")
	errUntimelyMessage     = errors.New("untimely msg")
)

// HandleWeakCoinProposal handles weakcoin proposal from gossip.
func (pd *ProtocolDriver) HandleWeakCoinProposal(ctx context.Context, peer p2p.Peer, msg []byte) pubsub.ValidationResult {
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

	logger = pd.logger.WithContext(ctx).WithFields(m.EpochID, log.String("miner_id", m.NodeID.ShortString()))
	logger.With().Debug("new beacon proposal", log.String("proposal", hex.EncodeToString(cropData(m.VRFSignature))))

	if _, err := pd.initEpochStateIfNotPresent(logger, m.EpochID); err != nil {
		return err
	}

	atxHeader, err := pd.verifyProposalMessage(logger, m)
	if err != nil {
		return err
	}

	cat, err := pd.classifyProposal(logger, m, atxHeader, receivedTime)
	if err != nil {
		return err
	}
	return pd.addProposal(m, cat)
}

func (pd *ProtocolDriver) classifyProposal(logger log.Log, m ProposalMessage, atxHeader *types.ActivationTxHeader, receivedTime time.Time) (category, error) {
	atxEpoch := atxHeader.PublishEpoch()
	nextEpochStart := pd.clock.LayerToTime((atxEpoch + 1).FirstLayer())

	logger = logger.WithFields(
		log.String("proposal", hex.EncodeToString(cropData(m.VRFSignature))),
		log.Time("atx_timestamp", atxHeader.Received),
		log.Stringer("next_epoch_start", nextEpochStart),
		log.Time("received_time", receivedTime),
		log.Duration("grace_period", pd.config.GracePeriodDuration),
	)

	// Each smesher partitions the valid proposals received in the previous epoch into three sets:
	// - Timely proposals: received up to δ after the end of the previous epoch.
	// - Delayed proposals: received between δ and 2δ after the end of the previous epoch.
	// - Late proposals: more than 2δ after the end of the previous epoch.
	// Note that honest users cannot disagree on timing by more than δ,
	// so if a proposal is timely for any honest user,
	// it cannot be late for any honest user (and vice versa).

	var (
		atxDelay      = atxHeader.Received.Sub(nextEpochStart)
		endTime       = pd.getProposalPhaseFinishedTime(m.EpochID)
		proposalDelay time.Duration
	)
	if endTime != (time.Time{}) {
		proposalDelay = receivedTime.Sub(endTime)
	}

	switch {
	case atxDelay <= 0 && proposalDelay <= 0:
		logger.Debug("received valid proposal: ATX delay %v, proposal delay %v", atxDelay, proposalDelay)
		return valid, nil
	case atxDelay <= pd.config.GracePeriodDuration && proposalDelay <= pd.config.GracePeriodDuration:
		logger.Debug("received potentially proposal: ATX delay %v, proposal delay %v", atxDelay, proposalDelay)
		return potentiallyValid, nil
	default:
		logger.Warning("received invalid proposal: ATX delay %v, proposal delay %v", atxDelay, proposalDelay)
	}
	return invalid, nil
}

func cropData(data []byte) []byte {
	shortened := data
	if types.BeaconSize < len(data) {
		shortened = data[:types.BeaconSize]
	}
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

func (pd *ProtocolDriver) verifyProposalMessage(logger log.Log, m ProposalMessage) (*types.ActivationTxHeader, error) {
	minerID := m.NodeID.ShortString()

	atxHeader, err := pd.cdb.GetEpochAtx(m.EpochID-1, m.NodeID)
	if errors.Is(err, sql.ErrNotFound) {
		logger.Warning("[proposal] miner has no ATX in previous epoch")
		return nil, fmt.Errorf("[proposal] no ATX in previous epoch (miner ID %v): %w", minerID, errMinerATXNotFound)
	}

	if err != nil {
		logger.With().Warning("[proposal] failed to get ATX header", log.Err(err))
		return nil, fmt.Errorf("[proposal] get ATX header (miner ID %v): %w", minerID, err)
	}

	nonce, err := pd.nonceFetcher.VRFNonce(m.NodeID, m.EpochID)
	if err != nil {
		logger.With().Warning("[proposal] failed to get VRF nonce", log.Err(err))
		return nil, fmt.Errorf("[proposal] get VRF nonce (miner ID %v): %w", minerID, err)
	}
	currentEpochProposal := buildProposal(logger, m.EpochID, nonce)
	if !pd.vrfVerifier.Verify(m.NodeID, currentEpochProposal, m.VRFSignature) {
		// TODO(nkryuchkov): attach telemetry
		logger.With().Warning("[proposal] failed to verify VRF signature")
		return nil, fmt.Errorf("[proposal] verify VRF (miner ID %v): %w", minerID, errVRFNotVerified)
	}

	vrfPK := signing.NewPublicKey(m.NodeID.Bytes())
	if err = pd.registerProposed(logger, m.EpochID, vrfPK); err != nil {
		logger.With().Warning("[proposal] failed to register miner proposed", log.Err(err))
		return nil, fmt.Errorf("[proposal] register proposal (miner ID %v): %w", minerID, err)
	}

	if !pd.proposalEligible(logger, m.EpochID, m.VRFSignature) {
		logger.With().Debug("[proposal] proposal not eligible", log.String("proposal", hex.EncodeToString(cropData(m.VRFSignature))))
		return nil, fmt.Errorf("[proposal] not eligible (miner ID %v): %w", minerID, errProposalNotEligible)
	}

	return atxHeader, nil
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

	minerPK, atxHeader, err := pd.verifyFirstVotes(ctx, m)
	if err != nil {
		return err
	}
	voteWeight := new(big.Int).SetUint64(atxHeader.GetWeight())

	logger.Debug("received first voting message, storing its votes")
	return pd.storeFirstVotes(m, minerPK, voteWeight)
}

func (pd *ProtocolDriver) verifyFirstVotes(ctx context.Context, m FirstVotingMessage) (*signing.PublicKey, *types.ActivationTxHeader, error) {
	logger := pd.logger.WithContext(ctx).WithFields(m.EpochID, types.FirstRound)
	messageBytes, err := codec.Encode(&m.FirstVotingMessageBody)
	if err != nil {
		logger.With().Fatal("failed to serialize first voting message", log.Err(err))
	}

	minerPK, err := pd.pubKeyExtractor.Extract(messageBytes, m.Signature)
	if err != nil {
		return nil, nil, fmt.Errorf("[round %v] recover ID %x: %w", types.FirstRound, m.Signature, err)
	}

	nodeID := types.BytesToNodeID(minerPK.Bytes())
	minerID := nodeID.ShortString()
	logger = logger.WithFields(log.String("miner_id", minerID))

	if err = pd.registerVoted(logger, m.EpochID, minerPK, types.FirstRound); err != nil {
		return nil, nil, fmt.Errorf("[round %v] register proposal (miner ID %v): %w", types.FirstRound, minerID, err)
	}

	atxHeader, err := pd.cdb.GetEpochAtx(m.EpochID-1, nodeID)
	if errors.Is(err, sql.ErrNotFound) {
		logger.Warning("miner has no ATX in the previous epoch")
		return nil, nil, fmt.Errorf("[round %v] no ATX in previous epoch (miner ID %v): %w", types.FirstRound, minerID, errMinerATXNotFound)
	}

	if err != nil {
		logger.With().Error("failed to get ATX", log.Err(err))
		return nil, nil, fmt.Errorf("[round %v] get ATX for epoch (miner ID %v): %w", types.FirstRound, minerID, err)
	}

	return minerPK, atxHeader, nil
}

func (pd *ProtocolDriver) storeFirstVotes(m FirstVotingMessage, minerPK *signing.PublicKey, voteWeight *big.Int) error {
	if !pd.isInProtocol() {
		pd.logger.Debug("beacon not in protocol, not storing first votes")
		return errProtocolNotRunning
	}

	pd.mu.Lock()
	defer pd.mu.Unlock()

	if _, ok := pd.states[m.EpochID]; !ok {
		return errEpochNotActive
	}

	for _, proposal := range m.ValidProposals {
		pd.states[m.EpochID].addVote(string(proposal), up, voteWeight)
	}

	for _, proposal := range m.PotentiallyValidProposals {
		pd.states[m.EpochID].addVote(string(proposal), down, voteWeight)
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

	minerPK, atxHeader, err := pd.verifyFollowingVotes(ctx, m)
	if err != nil {
		return err
	}
	voteWeight := new(big.Int).SetUint64(atxHeader.GetWeight())

	logger.Debug("received following voting message, counting its votes")
	if err = pd.storeFollowingVotes(m, minerPK, voteWeight); err != nil {
		logger.With().Warning("failed to store following votes", log.Err(err))
		return err
	}

	return nil
}

func (pd *ProtocolDriver) verifyFollowingVotes(ctx context.Context, m FollowingVotingMessage) (*signing.PublicKey, *types.ActivationTxHeader, error) {
	round := m.RoundID
	messageBytes, err := codec.Encode(&m.FollowingVotingMessageBody)
	if err != nil {
		pd.logger.With().Fatal("failed to serialize voting message", log.Err(err))
	}

	minerPK, err := pd.pubKeyExtractor.Extract(messageBytes, m.Signature)
	if err != nil {
		return nil, nil, fmt.Errorf("[round %v] recover ID from signature %x: %w", round, m.Signature, err)
	}

	nodeID := types.BytesToNodeID(minerPK.Bytes())
	minerID := nodeID.ShortString()
	logger := pd.logger.WithContext(ctx).WithFields(m.EpochID, round, log.String("miner_id", minerID))

	if err := pd.registerVoted(logger, m.EpochID, minerPK, m.RoundID); err != nil {
		return nil, nil, err
	}

	atxHeader, err := pd.cdb.GetEpochAtx(m.EpochID-1, nodeID)
	if errors.Is(err, sql.ErrNotFound) {
		logger.Warning("miner has no ATX in the previous epoch")
		return nil, nil, fmt.Errorf("[round %v] no ATX in previous epoch (miner ID %v): %w", round, minerID, errMinerATXNotFound)
	}

	if err != nil {
		logger.With().Error("failed to get ATX", log.Err(err))
		return nil, nil, fmt.Errorf("[round %v] get ATX for epoch (miner ID %v): %w", round, minerID, err)
	}

	return minerPK, atxHeader, nil
}

func (pd *ProtocolDriver) storeFollowingVotes(m FollowingVotingMessage, minerPK *signing.PublicKey, voteWeight *big.Int) error {
	if !pd.isInProtocol() {
		pd.logger.Debug("beacon not in protocol, not storing following votes")
		return errProtocolNotRunning
	}

	firstRoundVotes, err := pd.getFirstRoundVote(m.EpochID, minerPK)
	if err != nil {
		return fmt.Errorf("get miner first round votes %v: %w", minerPK.String(), err)
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

func (pd *ProtocolDriver) proposalEligible(logger log.Log, epoch types.EpochID, vrfSig []byte) bool {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	if _, ok := pd.states[epoch]; !ok {
		return false
	}

	eligible := pd.states[epoch].proposalChecker.IsProposalEligible(vrfSig)
	if !eligible {
		// the peer may have different total weight from us so that it passes threshold for the peer
		// but does not pass here
		proposal := hex.EncodeToString(cropData(vrfSig))
		logger.With().Warning("proposal doesn't pass threshold",
			log.String("proposal", proposal),
			log.Uint64("total_weight", pd.states[epoch].epochWeight))
	}
	return eligible
}

func (pd *ProtocolDriver) registerProposed(logger log.Log, epoch types.EpochID, minerPK *signing.PublicKey) error {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	if _, ok := pd.states[epoch]; !ok {
		return errEpochNotActive
	}
	return pd.states[epoch].registerProposed(logger, minerPK)
}

func (pd *ProtocolDriver) registerVoted(logger log.Log, epoch types.EpochID, minerPK *signing.PublicKey, round types.RoundID) error {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	if _, ok := pd.states[epoch]; !ok {
		return errEpochNotActive
	}
	return pd.states[epoch].registerVoted(logger, minerPK, round)
}
