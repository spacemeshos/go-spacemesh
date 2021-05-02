// Package hare implements the Hare Protocol.
package hare

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/spacemeshos/go-spacemesh/signing"
	"time"
)

const protoName = "HARE_PROTOCOL"

type role byte

const ( // constants of the different roles
	passive = role(0)
	active  = role(1)
	leader  = role(2)
)

// Rolacle is the roles oracle provider.
type Rolacle interface {
	Validate(ctx context.Context, layer types.LayerID, round int32, committeeSize int, id types.NodeID, sig []byte, eligibilityCount uint16) (bool, error)
	CalcEligibility(ctx context.Context, layer types.LayerID, round int32, committeeSize int, id types.NodeID, sig []byte) (uint16, error)
	Proof(ctx context.Context, layer types.LayerID, round int32) ([]byte, error)
	IsIdentityActiveOnConsensusView(edID string, layer types.LayerID) (bool, error)
}

// NetworkService provides the registration and broadcast abilities in the network.
type NetworkService interface {
	RegisterGossipProtocol(protocol string, prio priorityq.Priority) chan service.GossipMessage
	Broadcast(ctx context.Context, protocol string, payload []byte) error
}

// Signer provides signing and public-key getter.
type Signer interface {
	Sign(m []byte) []byte
	PublicKey() *signing.PublicKey
}

const (
	completed    = true
	notCompleted = false
)

// procReport is the termination report of the CP.
// It consists of the layer id, the set we agreed on (if available) and a flag to indicate if the CP completed.
type procReport struct {
	id        instanceID
	set       *Set
	completed bool
}

func (cpo procReport) ID() instanceID {
	return cpo.id
}

func (cpo procReport) Set() *Set {
	return cpo.set
}

func (cpo procReport) Completed() bool {
	return cpo.completed
}

func (proc *consensusProcess) report(completed bool) {
	proc.terminationReport <- procReport{proc.instanceID, proc.s, completed}
}

var _ TerminationOutput = (*procReport)(nil)

// State holds the current state of the consensus process (aka the participant).
type State struct {
	k           int32        // the round counter (k%4 is the round number)
	ki          int32        // indicates when S was first committed upon
	s           *Set         // the set of values
	certificate *certificate // the certificate
}

// StateQuerier provides a query to check if an Ed public key is active on the current consensus view.
// It returns true if the identity is active and false otherwise.
// An error is set iff the identity could not be checked for activeness.
type StateQuerier interface {
	IsIdentityActiveOnConsensusView(edID string, layer types.LayerID) (bool, error)
}

// Msg is the wrapper of the protocol's message.
// Messages are sent as type Message. Upon receiving, the public key is added to this wrapper (public key extraction).
type Msg struct {
	*Message
	PubKey    *signing.PublicKey
	RequestID string
}

func (m *Msg) String() string {
	return fmt.Sprintf("Pubkey: %v Message: %v", m.PubKey.ShortString(), m.Message.String())
}

// Bytes returns the message as bytes (without the public key).
// It panics if the message erred on unmarshal.
func (m *Msg) Bytes() []byte {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, m.Message); err != nil {
		log.Panic("could not marshal InnerMsg before send")
	}

	return w.Bytes()
}

// Upon receiving a protocol's message, we try to build the full message.
// The full message consists of the original message and the extracted public key.
// An extracted public key is considered valid if it represents an active identity for a consensus view.
func newMsg(ctx context.Context, hareMsg *Message, querier StateQuerier) (*Msg, error) {
	logger := log.AppLog.WithContext(ctx)

	// extract pub key
	pubKey, err := ed25519.ExtractPublicKey(hareMsg.InnerMsg.Bytes(), hareMsg.Sig)
	if err != nil {
		logger.With().Error("newMsg construction failed: could not extract public key",
			log.Err(err),
			log.Int("sig_len", len(hareMsg.Sig)))
		return nil, err
	}

	// query if identity is active
	pub := signing.NewPublicKey(pubKey)
	res, err := querier.IsIdentityActiveOnConsensusView(pub.String(), types.LayerID(hareMsg.InnerMsg.InstanceID))
	if err != nil {
		logger.With().Error("error while checking if identity is active",
			log.String("sender_id", pub.ShortString()),
			log.Err(err),
			types.LayerID(hareMsg.InnerMsg.InstanceID),
			log.String("msg_type", hareMsg.InnerMsg.Type.String()))
		return nil, errors.New("is identity active query failed")
	}

	// check query result
	if !res {
		logger.With().Error("identity is not active",
			log.String("sender_id", pub.ShortString()),
			types.LayerID(hareMsg.InnerMsg.InstanceID),
			log.String("msg_type", hareMsg.InnerMsg.Type.String()))
		return nil, errors.New("inactive identity")
	}

	msg := &Msg{Message: hareMsg, PubKey: pub}

	// add reqID from context
	if reqID, ok := log.ExtractRequestID(ctx); ok {
		msg.RequestID = reqID
	} else {
		logger.Warning("no requestID in context, cannot add to new hare message")
	}

	return msg, nil
}

// consensusProcess is an entity (a single participant) in the Hare protocol.
// Once started, the CP iterates through the rounds until consensus is reached or the instance is cancelled.
// The output is then written to the provided TerminationReport channel.
// If the consensus process is canceled one should not expect the output to be written to the output channel.
type consensusProcess struct {
	log.Log
	State
	Closer
	instanceID        instanceID // the layer id
	oracle            Rolacle    // the roles oracle provider
	signing           Signer
	nid               types.NodeID
	network           NetworkService
	isStarted         bool
	inbox             chan *Msg
	terminationReport chan TerminationOutput
	validator         messageValidator
	preRoundTracker   *preRoundTracker
	statusesTracker   *statusTracker
	proposalTracker   proposalTrackerProvider
	commitTracker     commitTrackerProvider
	notifyTracker     *notifyTracker
	cfg               config.Config
	pending           map[string]*Msg // buffer for early messages that are pending process
	notifySent        bool            // flag to set in case a notification had already been sent by this instance
	mTracker          *msgsTracker    // tracks valid messages
	terminating       bool
	eligibilityCount  uint16
}

// newConsensusProcess creates a new consensus process instance.
func newConsensusProcess(cfg config.Config, instanceID instanceID, s *Set, oracle Rolacle, stateQuerier StateQuerier,
	layersPerEpoch uint16, signing Signer, nid types.NodeID, p2p NetworkService,
	terminationReport chan TerminationOutput, ev roleValidator, logger log.Log) *consensusProcess {
	msgsTracker := newMsgsTracker()
	proc := &consensusProcess{
		State:             State{-1, -1, s.Clone(), nil},
		Closer:            NewCloser(),
		instanceID:        instanceID,
		oracle:            oracle,
		signing:           signing,
		nid:               nid,
		network:           p2p,
		preRoundTracker:   newPreRoundTracker(cfg.F+1, cfg.N, logger),
		notifyTracker:     newNotifyTracker(cfg.N),
		cfg:               cfg,
		terminationReport: terminationReport,
		pending:           make(map[string]*Msg, cfg.N),
		Log:               logger,
		mTracker:          msgsTracker,
	}
	proc.validator = newSyntaxContextValidator(signing, cfg.F+1, proc.statusValidator(), stateQuerier, layersPerEpoch, ev, msgsTracker, logger)

	return proc
}

// Returns the iteration number from a given round counter
func iterationFromCounter(roundCounter int32) int32 {
	return roundCounter / 4
}

// Start the consensus process.
// It starts the PreRound round and then iterates through the rounds until consensus is reached or the instance is cancelled.
// It is assumed that the inbox is set before the call to Start.
// It returns an error if Start has been called more than once, the set size is zero (no values) or the inbox is nil.
func (proc *consensusProcess) Start(ctx context.Context) error {
	logger := proc.WithContext(ctx)
	if proc.isStarted { // called twice on same instance
		logger.Error("consensusProcess has already been started")
		return startInstanceError(errors.New("instance already started"))
	}

	if proc.s.Size() == 0 { // empty set is not valid
		logger.Error("consensusProcess cannot be started with an empty set")
		return startInstanceError(errors.New("instance started with an empty set"))
	}

	if proc.inbox == nil { // no inbox
		logger.Error("consensusProcess cannot be started with nil inbox")
		return startInstanceError(errors.New("instance started with nil inbox"))
	}

	proc.isStarted = true

	go proc.eventLoop(ctx)

	return nil
}

// ID returns the instance id.
func (proc *consensusProcess) ID() instanceID {
	return proc.instanceID
}

// SetInbox sets the inbox channel for incoming messages.
func (proc *consensusProcess) SetInbox(inbox chan *Msg) {
	if inbox == nil {
		proc.Error("consensusProcess tried to SetInbox with nil")
		return
	}

	proc.inbox = inbox
}

// runs the main loop of the protocol
func (proc *consensusProcess) eventLoop(ctx context.Context) {
	logger := proc.WithContext(ctx)
	logger.With().Info("consensus process started",
		log.Int("hare_n", proc.cfg.N),
		log.Int("f", proc.cfg.F),
		log.String("duration", (time.Duration(proc.cfg.RoundDuration)*time.Second).String()),
		types.LayerID(proc.instanceID),
		log.Int("exp_leaders", proc.cfg.ExpectedLeaders),
		log.String("current_set", proc.s.String()),
		log.Int("set_size", proc.s.Size()))

	// start the timer
	timer := time.NewTimer(time.Duration(proc.cfg.RoundDuration) * time.Second)
	defer timer.Stop()

	// check participation and send message
	go func() {
		// check participation
		if proc.shouldParticipate(ctx) {
			// set pre-round InnerMsg and send
			builder, err := proc.initDefaultBuilder(proc.s)
			if err != nil {
				logger.With().Error("init default builder failed", log.Err(err))
				return
			}
			m := builder.SetType(pre).Sign(proc.signing).Build()
			proc.sendMessage(ctx, m)
		} else {
			logger.With().Info("should not participate",
				log.Int32("current_k", proc.k),
				types.LayerID(proc.instanceID))
		}
	}()

PreRound:
	for {
		select {
		// listen to pre-round Messages
		case msg := <-proc.inbox:
			proc.handleMessage(ctx, msg)
		case <-timer.C:
			break PreRound
		case <-proc.CloseChannel():
			logger.With().Info("terminating during preround: received termination signal",
				log.Int32("current_k", proc.k),
				types.LayerID(proc.instanceID))
			return
		}
	}
	logger.With().Debug("preround ended, filtering preliminary set",
		types.LayerID(proc.instanceID),
		log.Int("set_size", proc.s.Size()))
	proc.preRoundTracker.FilterSet(proc.s)
	logger.With().Debug("preround set size after filtering",
		types.LayerID(proc.instanceID),
		log.Int("set_size", proc.s.Size()))
	if proc.s.Size() == 0 {
		logger.Event().Error("preround ended with empty set",
			types.LayerID(proc.instanceID),
			log.String("tracked_values", proc.preRoundTracker.tracker.String()),
			log.Uint32("threshold", proc.preRoundTracker.threshold),
		)
	} else {
		logger.With().Info("preround ended",
			log.Int("set_size", proc.s.Size()),
			types.LayerID(proc.instanceID))
	}
	proc.advanceToNextRound(ctx) // K was initialized to -1, K should be 0

	// start first iteration
	proc.onRoundBegin(ctx)
	ticker := time.NewTicker(time.Duration(proc.cfg.RoundDuration) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg := <-proc.inbox: // msg event
			proc.handleMessage(ctx, msg)
			if proc.terminating {
				return
			}
		case <-ticker.C: // next round event
			proc.onRoundEnd(ctx)
			proc.advanceToNextRound(ctx)

			// exit if we reached the limit on number of iterations
			if proc.k/4 >= int32(proc.cfg.LimitIterations) {
				logger.With().Warning("terminating: reached iterations limit",
					log.Int("limit", proc.cfg.LimitIterations),
					log.Int32("current_k", proc.k),
					types.LayerID(proc.instanceID))
				proc.report(notCompleted)
				return
			}

			proc.onRoundBegin(ctx)
		case <-proc.CloseChannel(): // close event
			logger.With().Info("terminating: received termination signal",
				log.Int32("current_k", proc.k),
				types.LayerID(proc.instanceID))
			proc.report(notCompleted)
			return
		}
	}
}

// handles a message that has arrived early
func (proc *consensusProcess) onEarlyMessage(ctx context.Context, m *Msg) {
	logger := proc.WithContext(ctx)

	if m == nil {
		logger.Error("onEarlyMessage called with nil")
		return
	}

	if m.Message == nil {
		logger.Error("onEarlyMessage called with nil message")
		return
	}

	if m.InnerMsg == nil {
		logger.Error("onEarlyMessage called with nil inner message")
		return
	}

	pub := m.PubKey
	if _, exist := proc.pending[pub.String()]; exist { // ignore, already received
		logger.With().Warning("already received message from sender",
			log.String("sender_id", pub.ShortString()))
		return
	}

	proc.pending[pub.String()] = m
}

// the very first step of handling a message
func (proc *consensusProcess) handleMessage(ctx context.Context, m *Msg) {
	logger := proc.WithContext(ctx).WithFields(
		log.String("msg_type", m.InnerMsg.Type.String()),
		log.FieldNamed("sender_id", m.PubKey),
		log.Int32("current_k", proc.k),
		log.Int32("msg_k", m.InnerMsg.K),
		types.LayerID(proc.instanceID))

	// Try to extract reqID from message and restore it to context
	if m.RequestID == "" {
		logger.Warning("no reqID found in hare message, cannot restore to context")
	} else {
		ctx = log.WithRequestID(ctx, m.RequestID)
		logger = logger.WithContext(ctx)
	}

	// Note: instanceID is already verified by the broker
	logger.Debug("consensus process received message")

	// validate context
	if err := proc.validator.ContextuallyValidateMessage(ctx, m, proc.k); err != nil {
		// early message, keep for later
		if err == errEarlyMsg {
			logger.With().Debug("early message detected, keeping", log.Err(err))

			// validate syntax for early messages
			if !proc.validator.SyntacticallyValidateMessage(ctx, m) {
				logger.Warning("early message failed syntactic validation, discarding")
				return
			}

			proc.onEarlyMessage(ctx, m)
			return
		}

		// not an early message but also contextually invalid
		logger.With().Error("late message failed contextual validation, discarding", log.Err(err))
		return
	}

	// validate syntax for contextually valid messages
	if !proc.validator.SyntacticallyValidateMessage(ctx, m) {
		logger.Warning("message failed syntactic validation, discarding")
		return
	}

	// warn on late pre-round msgs
	if m.InnerMsg.Type == pre && proc.k != -1 {
		logger.Warning("encountered late preround message")
	}

	// valid, continue to process msg by type
	proc.processMsg(ctx, m)
}

// process the message by its type
func (proc *consensusProcess) processMsg(ctx context.Context, m *Msg) {
	proc.WithContext(ctx).With().Debug("processing message",
		log.String("msg_type", m.InnerMsg.Type.String()),
		log.Int("num_values", len(m.InnerMsg.Values)))
	// TODO: fix metrics
	// metrics.MessageTypeCounter.With("type_id", m.InnerMsg.Type.String(), "layer", strconv.FormatUint(uint64(m.InnerMsg.InstanceID), 10), "reporter", "processMsg").Add(1)

	switch m.InnerMsg.Type {
	case pre:
		proc.processPreRoundMsg(ctx, m)
	case status: // end of round 1
		proc.processStatusMsg(ctx, m)
	case proposal: // end of round 2
		proc.processProposalMsg(ctx, m)
	case commit: // end of round 3
		proc.processCommitMsg(ctx, m)
	case notify: // end of round 4
		proc.processNotifyMsg(ctx, m)
	default:
		proc.WithContext(ctx).With().Warning("unknown message type",
			log.String("msg_type", m.InnerMsg.Type.String()),
			log.String("sender_id", m.PubKey.ShortString()))
	}
}

// sends a message to the network.
// Returns true if the message is assumed to be sent, false otherwise.
func (proc *consensusProcess) sendMessage(ctx context.Context, msg *Msg) bool {
	// invalid msg
	if msg == nil {
		proc.WithContext(ctx).Error("sendMessage was called with nil")
		return false
	}

	// generate a new requestID for this message
	ctx = log.WithNewRequestID(ctx,
		types.LayerID(proc.instanceID),
		log.Int32("msg_k", msg.InnerMsg.K),
		log.String("msg_type", msg.InnerMsg.Type.String()),
		log.Int("eligibility_count", int(msg.InnerMsg.EligibilityCount)),
		log.String("current_set", proc.s.String()),
		log.Int32("current_k", proc.k),
	)
	logger := proc.WithContext(ctx)

	if err := proc.network.Broadcast(ctx, protoName, msg.Bytes()); err != nil {
		logger.With().Error("could not broadcast round message", log.Err(err))
		return false
	}

	logger.Info("should participate: message sent")
	return true
}

// logic of the end of a round by the round type
func (proc *consensusProcess) onRoundEnd(ctx context.Context) {
	logger := proc.WithContext(ctx).WithFields(
		log.Int32("current_k", proc.k),
		types.LayerID(proc.instanceID),
	)
	logger.Debug("end of round")

	// reset trackers
	switch proc.currentRound() {
	case statusRound:
		proc.endOfStatusRound()
	case proposalRound:
		s := proc.proposalTracker.ProposedSet()
		sStr := "nil"
		if s != nil {
			sStr = s.String()
		}
		logger.Event().Info("proposal round ended",
			log.Int("set_size", proc.s.Size()),
			log.String("proposed_set", sStr),
			log.Bool("is_conflicting", proc.proposalTracker.IsConflicting()))
	case commitRound:
		logger.With().Info("commit round ended", log.Int("set_size", proc.s.Size()))
	}
}

// advances the state to the next round
func (proc *consensusProcess) advanceToNextRound(ctx context.Context) {
	proc.k++
	if proc.k >= 4 && proc.k%4 == 0 {
		proc.WithContext(ctx).Event().Warning("starting new iteration",
			log.Int32("current_k", proc.k),
			types.LayerID(proc.instanceID))
	}
}

func (proc *consensusProcess) beginStatusRound(ctx context.Context) {
	proc.statusesTracker = newStatusTracker(proc.cfg.F+1, proc.cfg.N)
	proc.statusesTracker.Log = proc.Log

	// check participation
	if !proc.shouldParticipate(ctx) {
		return
	}

	b, err := proc.initDefaultBuilder(proc.s)
	if err != nil {
		proc.With().Error("init default builder failed", log.Err(err))
		return
	}
	statusMsg := b.SetType(status).Sign(proc.signing).Build()
	proc.sendMessage(ctx, statusMsg)
}

func (proc *consensusProcess) beginProposalRound(ctx context.Context) {
	proc.proposalTracker = newProposalTracker(proc.Log)

	// done with building proposal, reset statuses tracking
	defer func() { proc.statusesTracker = nil }()

	if proc.statusesTracker.IsSVPReady() && proc.shouldParticipate(ctx) {
		builder, err := proc.initDefaultBuilder(proc.statusesTracker.ProposalSet(defaultSetSize))
		if err != nil {
			proc.With().Error("init default builder failed", log.Err(err))
			return
		}
		svp := proc.statusesTracker.BuildSVP()
		if svp != nil {
			proposalMsg := builder.SetType(proposal).SetSVP(svp).Sign(proc.signing).Build()
			proc.sendMessage(ctx, proposalMsg)
		} else {
			proc.Error("failed to build SVP (nil) after verifying SVP is ready")
		}
	}
}

func (proc *consensusProcess) beginCommitRound(ctx context.Context) {
	proposedSet := proc.proposalTracker.ProposedSet()

	// proposedSet may be nil, in such case the tracker will ignore Messages
	proc.commitTracker = newCommitTracker(proc.cfg.F+1, proc.cfg.N, proposedSet) // track commits for proposed set

	if proposedSet != nil { // has proposal to commit on

		// check participation
		if !proc.shouldParticipate(ctx) {
			return
		}

		builder, err := proc.initDefaultBuilder(proposedSet)
		if err != nil {
			proc.WithContext(ctx).With().Error("init default builder failed", log.Err(err))
			return
		}
		builder = builder.SetType(commit).Sign(proc.signing)
		commitMsg := builder.Build()
		proc.sendMessage(ctx, commitMsg)
	}
}

func (proc *consensusProcess) beginNotifyRound(ctx context.Context) {
	logger := proc.WithContext(ctx).WithFields(types.LayerID(proc.instanceID))

	// release proposal & commit trackers
	defer func() {
		proc.commitTracker = nil
		proc.proposalTracker = nil
	}()

	// send notify message only once
	if proc.notifySent {
		logger.Info("begin notify round: notify already sent")
		return
	}

	if proc.proposalTracker.IsConflicting() {
		logger.Warning("begin notify round: proposal is conflicting")
		return
	}

	if !proc.commitTracker.HasEnoughCommits() {
		logger.With().Warning("begin notify round: not enough commits",
			log.Int("expected", proc.cfg.F+1),
			log.Int("actual", proc.commitTracker.CommitCount()))
		return
	}

	cert := proc.commitTracker.BuildCertificate()
	if cert == nil {
		logger.Error("begin notify round: BuildCertificate returned nil")
		return
	}

	s := proc.proposalTracker.ProposedSet()
	if s == nil {
		logger.Error("begin notify round: ProposedSet returned nil")
		return
	}

	// update set & matching certificate
	proc.s = s
	proc.certificate = cert

	// check participation
	if !proc.shouldParticipate(ctx) {
		return
	}

	// build & send notify message
	builder, err := proc.initDefaultBuilder(proc.s)
	if err != nil {
		logger.With().Error("init default builder failed", log.Err(err))
		return
	}

	builder = builder.SetType(notify).SetCertificate(proc.certificate).Sign(proc.signing)
	notifyMsg := builder.Build()
	logger.With().Debug("sending notify message", notifyMsg)
	if proc.sendMessage(ctx, notifyMsg) { // on success, mark sent
		proc.notifySent = true
	}
}

// passes all pending messages to the inbox of the process so they will be handled
func (proc *consensusProcess) handlePending(pending map[string]*Msg) {
	for _, m := range pending {
		proc.inbox <- m
	}
}

// runs the logic of the beginning of a round by its type
// pending messages are passed for handling
func (proc *consensusProcess) onRoundBegin(ctx context.Context) {
	// reset trackers
	switch proc.currentRound() {
	case statusRound:
		proc.beginStatusRound(ctx)
	case proposalRound:
		proc.beginProposalRound(ctx)
	case commitRound:
		proc.beginCommitRound(ctx)
	case notifyRound:
		proc.beginNotifyRound(ctx)
	default:
		proc.Panic("current round out of bounds. Expected: 0-3, Found: %v", proc.currentRound())
	}

	if len(proc.pending) == 0 { // no pending messages
		return
	}
	// handle pending messages
	pendingProcess := proc.pending
	proc.pending = make(map[string]*Msg, proc.cfg.N)
	go proc.handlePending(pendingProcess)
}

// init a new message builder with the current state (s, k, ki) for this instance
func (proc *consensusProcess) initDefaultBuilder(s *Set) (*messageBuilder, error) {
	builder := newMessageBuilder().SetInstanceID(proc.instanceID)
	builder = builder.SetRoundCounter(proc.k).SetKi(proc.ki).SetValues(s)
	proof, err := proc.oracle.Proof(context.TODO(), types.LayerID(proc.instanceID), proc.k)
	if err != nil {
		proc.With().Error("could not initialize default builder", log.Err(err))
		return nil, err
	}
	builder.SetRoleProof(proof)
	builder.SetEligibilityCount(proc.eligibilityCount)

	return builder, nil
}

func (proc *consensusProcess) processPreRoundMsg(ctx context.Context, msg *Msg) {
	proc.preRoundTracker.OnPreRound(ctx, msg)
}

func (proc *consensusProcess) processStatusMsg(ctx context.Context, msg *Msg) {
	// record status
	proc.statusesTracker.RecordStatus(ctx, msg)
}

func (proc *consensusProcess) processProposalMsg(ctx context.Context, msg *Msg) {
	currRnd := proc.currentRound()

	if currRnd == proposalRound { // regular proposal
		proc.proposalTracker.OnProposal(ctx, msg)
	} else if currRnd == commitRound { // late proposal
		proc.proposalTracker.OnLateProposal(ctx, msg)
	} else {
		proc.WithContext(ctx).With().Error("received proposal message for processing in an invalid context",
			log.Int32("current_k", proc.k),
			log.Int32("msg_k", msg.InnerMsg.K))
	}
}

func (proc *consensusProcess) processCommitMsg(ctx context.Context, msg *Msg) {
	proc.mTracker.Track(msg) // a commit msg passed for processing is assumed to be valid
	proc.commitTracker.OnCommit(msg)
}

func (proc *consensusProcess) processNotifyMsg(ctx context.Context, msg *Msg) {
	s := NewSet(msg.InnerMsg.Values)

	if ignored := proc.notifyTracker.OnNotify(msg); ignored {
		proc.WithContext(ctx).With().Warning("ignoring notification",
			log.String("sender_id", msg.PubKey.ShortString()))
		return
	}

	if proc.currentRound() == notifyRound { // not necessary to update otherwise
		// we assume that this expression was checked before
		if msg.InnerMsg.Cert.AggMsgs.Messages[0].InnerMsg.K >= proc.ki { // update state iff K >= Ki
			proc.s = s
			proc.certificate = msg.InnerMsg.Cert
			proc.ki = msg.InnerMsg.Ki
		}
	}

	if proc.notifyTracker.NotificationsCount(s) < proc.cfg.F+1 { // not enough
		proc.WithContext(ctx).With().Debug("not enough notifications for termination",
			log.Int("expected", proc.cfg.F+1),
			log.Int("actual", proc.notifyTracker.NotificationsCount(s)))
		return
	}

	// enough notifications, should terminate
	proc.s = s // update to the agreed set
	proc.WithContext(ctx).Event().Info("consensus process terminated",
		log.String("current_set", proc.s.String()),
		log.Int32("current_k", proc.k),
		types.LayerID(proc.instanceID),
		log.Int("set_size", proc.s.Size()), log.Int32("K", proc.k))
	proc.report(completed)
	close(proc.CloseChannel())
	proc.terminating = true
}

func (proc *consensusProcess) currentRound() int {
	return int(proc.k % 4)
}

// returns a function to validate status messages
func (proc *consensusProcess) statusValidator() func(m *Msg) bool {
	validate := func(m *Msg) bool {
		s := NewSet(m.InnerMsg.Values)
		if m.InnerMsg.Ki == -1 { // no certificates, validate by pre-round msgs
			if proc.preRoundTracker.CanProveSet(s) { // can prove s
				return true
			}
		} else { // Ki>=0, we should have received a certificate for that set
			if proc.notifyTracker.HasCertificate(m.InnerMsg.Ki, s) { // can prove s
				return true
			}
		}
		return false
	}

	return validate
}

func (proc *consensusProcess) endOfStatusRound() {
	// validate and track wrapper for validation func
	valid := proc.statusValidator()
	vtFunc := func(m *Msg) bool {
		if valid(m) {
			proc.mTracker.Track(m)
			return true
		}

		return false
	}

	// assumption: AnalyzeStatuses calls vtFunc for every recorded status message
	before := time.Now()
	proc.statusesTracker.AnalyzeStatuses(vtFunc)
	proc.Event().Info("status round ended",
		log.Bool("is_svp_ready", proc.statusesTracker.IsSVPReady()),
		types.LayerID(proc.instanceID),
		log.Int("set_size", proc.s.Size()),
		log.String("analyze_duration", time.Since(before).String()))
}

// checks if we should participate in the current round
// returns true if we should participate, false otherwise
func (proc *consensusProcess) shouldParticipate(ctx context.Context) bool {
	logger := proc.WithContext(ctx)

	// query if identity is active
	res, err := proc.oracle.IsIdentityActiveOnConsensusView(proc.signing.PublicKey().String(), types.LayerID(proc.instanceID))
	if err != nil {
		logger.With().Error("should not participate: error checking our identity for activeness",
			log.Err(err), types.LayerID(proc.instanceID))
		return false
	}

	if !res {
		logger.With().Info("should not participate: identity is not active",
			types.LayerID(proc.instanceID))
		return false
	}

	currentRole := proc.currentRole(ctx)
	if currentRole == passive {
		logger.With().Info("should not participate: passive",
			log.Int32("current_k", proc.k), types.LayerID(proc.instanceID))
		return false
	}

	// should participate
	logger.With().Info("should participate",
		log.Int32("current_k", proc.k), types.LayerID(proc.instanceID),
		log.Bool("leader", currentRole == leader),
		log.Uint32("eligibility_count", uint32(proc.eligibilityCount)),
	)
	return true
}

// Returns the role matching the current round if eligible for this round, false otherwise
func (proc *consensusProcess) currentRole(ctx context.Context) role {
	logger := proc.WithContext(ctx)
	proof, err := proc.oracle.Proof(ctx, types.LayerID(proc.instanceID), proc.k)
	if err != nil {
		logger.With().Error("could not retrieve eligibility proof from oracle", log.Err(err))
		return passive
	}

	eligibilityCount, err := proc.oracle.CalcEligibility(ctx, types.LayerID(proc.instanceID),
		proc.k, expectedCommitteeSize(proc.k, proc.cfg.N, proc.cfg.ExpectedLeaders), proc.nid, proof)
	if err != nil {
		logger.With().Error("failed to check eligibility", log.Err(err), types.LayerID(proc.instanceID))
		return passive
	}

	proc.eligibilityCount = eligibilityCount
	if eligibilityCount > 0 { // eligible
		if proc.currentRound() == proposalRound {
			return leader
		}
		return active
	}

	return passive
}

// Returns the expected committee size for the given round assuming maxExpActives is the default size
func expectedCommitteeSize(k int32, maxExpActive, expLeaders int) int {
	if k%4 == proposalRound {
		return expLeaders // expected number of leaders
	}

	// N actives in any other case
	return maxExpActive
}
