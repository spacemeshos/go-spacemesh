// Package hare implements the Hare Protocol.
package hare

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	// RoundsPerIteration is the number of rounds per iteration in the hare protocol.
	RoundsPerIteration = 4
)

type role byte

const ( // constants of the different roles
	passive = role(0)
	active  = role(1)
	leader  = role(2)
)

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
type procReport struct {
	id        types.LayerID // layer id
	set       *Set          // agreed-upon set
	coinflip  bool          // weak coin value
	completed bool          // whether the CP completed
}

func (cpo procReport) ID() types.LayerID {
	return cpo.id
}

func (cpo procReport) Set() *Set {
	return cpo.set
}

func (cpo procReport) Coinflip() bool {
	return cpo.coinflip
}

func (cpo procReport) Completed() bool {
	return cpo.completed
}

func (proc *consensusProcess) report(completed bool) {
	proc.terminationReport <- procReport{proc.layer, proc.value, proc.preRoundTracker.coinflip, completed}
}

var _ TerminationOutput = (*procReport)(nil)

// State holds the current state of the consensus process (aka the participant).
type State struct {
	round          uint32       // the round counter (round%4 is the round number); it should be first in struct for alignment because atomics are used (K)
	committedRound uint32       // indicates when value (S) was first committed upon (Ki)
	value          *Set         // the set of values (S)
	certificate    *Certificate // the certificate
}

// Msg is the wrapper of the protocol's message.
// Messages are sent as type Message. Upon receiving, the public key is added to this wrapper (public key extraction).
type Msg struct {
	Message
	PubKey    *signing.PublicKey
	RequestID string
}

// Bytes returns the message as bytes (without the public key).
// It panics if the message erred on unmarshal.
func (m *Msg) Bytes() []byte {
	buf, err := codec.Encode(&m.Message)
	if err != nil {
		log.With().Fatal("failed to encode Message", log.Err(err))
	}
	return buf
}

// Upon receiving a protocol message, we try to build the full message.
// The full message consists of the original message and the extracted public key.
// An extracted public key is considered valid if it represents an active identity for a consensus view.
func newMsg(ctx context.Context, logger log.Log, hareMsg Message, querier stateQuerier) (*Msg, error) {
	logger = logger.WithContext(ctx)

	// extract pub key
	nodeId, err := types.ExtractNodeIDFromSig(hareMsg.InnerMsg.Bytes(), hareMsg.Signature)
	if err != nil {
		logger.With().Error("failed to extract public key",
			log.Err(err),
			log.Int("sig_len", len(hareMsg.Signature)))
		return nil, fmt.Errorf("extract ed25519 pubkey: %w", err)
	}

	// query if identity is active
	res, err := querier.IsIdentityActiveOnConsensusView(ctx, nodeId, hareMsg.InnerMsg.Layer)
	if err != nil {
		logger.With().Error("failed to check if identity is active",
			log.String("sender_id", nodeId.ShortString()),
			log.Err(err),
			hareMsg.InnerMsg.Layer,
			log.String("msg_type", hareMsg.InnerMsg.Type.String()))
		return nil, fmt.Errorf("check active identity: %w", err)
	}

	// check query result
	if !res {
		logger.With().Warning("identity is not active",
			log.String("sender_id", nodeId.ShortString()),
			hareMsg.InnerMsg.Layer,
			log.String("msg_type", hareMsg.InnerMsg.Type.String()))
		return nil, errors.New("inactive identity")
	}

	msg := &Msg{Message: hareMsg, PubKey: signing.NewPublicKey(nodeId.Bytes())}

	// add reqID from context
	if reqID, ok := log.ExtractRequestID(ctx); ok {
		msg.RequestID = reqID
	} else {
		logger.Warning("no requestID in context, cannot add to new hare message")
	}

	return msg, nil
}

// consensusProcess is an entity (a single participant) in the Hare protocol.
// Once started, the CP iterates through the rounds until consensus is reached or the instance is canceled.
// The output is then written to the provided TerminationReport channel.
// If the consensus process is canceled one should not expect the output to be written to the output channel.
type consensusProcess struct {
	log.Log
	State

	ctx               context.Context
	cancel            context.CancelFunc
	eg                errgroup.Group
	mu                sync.RWMutex
	layer             types.LayerID
	oracle            Rolacle // the roles oracle provider
	signing           Signer
	nid               types.NodeID
	publisher         pubsub.Publisher
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
	mTracker          *msgsTracker    // tracks valid messages
	eligibilityCount  uint16
	clock             RoundClock
}

// newConsensusProcess creates a new consensus process instance.
func newConsensusProcess(cfg config.Config, layer types.LayerID, s *Set, oracle Rolacle, stateQuerier stateQuerier,
	layersPerEpoch uint16, signing Signer, nid types.NodeID, p2p pubsub.Publisher,
	terminationReport chan TerminationOutput,
	ev roleValidator, clock RoundClock, logger log.Log,
) *consensusProcess {
	msgsTracker := newMsgsTracker()
	proc := &consensusProcess{
		State:             State{preRound, preRound, s.Clone(), nil},
		layer:             layer,
		oracle:            oracle,
		signing:           signing,
		nid:               nid,
		publisher:         p2p,
		preRoundTracker:   newPreRoundTracker(cfg.F+1, cfg.N, logger),
		cfg:               cfg,
		terminationReport: terminationReport,
		pending:           make(map[string]*Msg, cfg.N),
		Log:               logger,
		mTracker:          msgsTracker,
		clock:             clock,
	}
	// provide a default context. overwritten when the consensus process is started.
	proc.ctx, proc.cancel = context.WithCancel(context.Background())
	proc.validator = newSyntaxContextValidator(signing, cfg.F+1, proc.statusValidator(), stateQuerier, layersPerEpoch, ev, msgsTracker, logger)

	return proc
}

// Returns the iteration number from a given round counter.
func iterationFromCounter(roundCounter uint32) uint32 {
	return roundCounter / RoundsPerIteration
}

// Start the consensus process.
// It starts the PreRound round and then iterates through the rounds until consensus is reached or the instance is canceled.
// It is assumed that the inbox is set before the call to Start.
// It returns an error if Start has been called more than once or the inbox is nil.
func (proc *consensusProcess) Start(ctx context.Context) error {
	if proc.isStarted { // called twice on same instance
		return fmt.Errorf("consensus process already started for layer %v", proc.layer)
	}

	if proc.inbox == nil { // no inbox
		return fmt.Errorf("consensus process for layer %v is missing inbox", proc.layer)
	}

	proc.isStarted = true
	if proc.cancel != nil {
		// GC the default cancel func
		proc.cancel()
	}
	proc.ctx, proc.cancel = context.WithCancel(ctx)
	proc.eg.Go(func() error {
		proc.eventLoop()
		return nil
	})

	return nil
}

// ID returns the instance id.
func (proc *consensusProcess) ID() types.LayerID {
	return proc.layer
}

// SetInbox sets the inbox channel for incoming messages.
func (proc *consensusProcess) SetInbox(inbox chan *Msg) {
	if inbox == nil {
		proc.Fatal("invalid argument for inbox")
	}
	proc.inbox = inbox
}

func (proc *consensusProcess) terminate() {
	proc.cancel()
	proc.eg.Wait()
}

func (proc *consensusProcess) terminating() bool {
	select {
	case <-proc.ctx.Done():
		return true
	default:
		return false
	}
}

// runs the main loop of the protocol.
func (proc *consensusProcess) eventLoop() {
	ctx := proc.ctx
	logger := proc.WithContext(ctx).WithFields(proc.layer)
	logger.With().Info("consensus process started",
		log.String("current_set", proc.value.String()),
		log.Int("set_size", proc.value.Size()))

	// check participation and send message
	proc.eg.Go(func() error {
		// check participation
		if proc.shouldParticipate(ctx) {
			// set pre-round InnerMsg and send
			builder, err := proc.initDefaultBuilder(proc.value)
			if err != nil {
				logger.With().Error("failed to init msg builder", log.Err(err))
				return nil
			}
			m := builder.SetType(pre).Sign(proc.signing).Build()
			proc.sendMessage(ctx, m)
		} else {
			logger.With().Debug("should not participate",
				log.Uint32("current_round", proc.getRound()))
		}
		return nil
	})

	endOfRound := proc.clock.AwaitEndOfRound(preRound)

PreRound:
	for {
		select {
		// listen to pre-round Messages
		case msg := <-proc.inbox:
			proc.handleMessage(ctx, msg)
		case <-endOfRound:
			break PreRound
		case <-proc.ctx.Done():
			logger.With().Info("terminating: received signal during preround",
				log.Uint32("current_round", proc.getRound()))
			return
		}
	}
	logger.With().Debug("preround ended, filtering preliminary set",
		log.Int("set_size", proc.value.Size()))
	proc.preRoundTracker.FilterSet(proc.value)
	if proc.value.Size() == 0 {
		logger.Event().Warning("preround ended with empty set")
	} else {
		logger.With().Debug("preround ended",
			log.Int("set_size", proc.value.Size()))
	}
	proc.advanceToNextRound(ctx) // K was initialized to -1, K should be 0

	// start first iteration
	proc.onRoundBegin(ctx)
	endOfRound = proc.clock.AwaitEndOfRound(proc.getRound())

	for {
		select {
		case msg := <-proc.inbox: // msg event
			if proc.terminating() {
				return
			}
			proc.handleMessage(ctx, msg)

		case <-endOfRound: // next round event
			proc.onRoundEnd(ctx)
			if proc.terminating() {
				return
			}
			proc.advanceToNextRound(ctx)

			// exit if we reached the limit on number of iterations
			round := proc.getRound()
			if round >= uint32(proc.cfg.LimitIterations)*RoundsPerIteration {
				logger.With().Warning("terminating: reached iterations limit",
					log.Int("limit", proc.cfg.LimitIterations),
					log.Uint32("current_round", round))
				proc.report(notCompleted)
				proc.terminate()
			}
			proc.onRoundBegin(ctx)
			endOfRound = proc.clock.AwaitEndOfRound(round)

		case <-proc.ctx.Done(): // close event
			logger.With().Info("terminating: received signal",
				log.Uint32("current_round", proc.getRound()))
			return
		}
	}
}

// handles a message that has arrived early.
func (proc *consensusProcess) onEarlyMessage(ctx context.Context, m *Msg) {
	logger := proc.WithContext(ctx)

	if m == nil {
		logger.Error("onEarlyMessage called with nil")
		return
	}

	if m.Message.InnerMsg == nil {
		logger.Error("onEarlyMessage called with nil inner message")
		return
	}

	if m.PubKey == nil {
		logger.Error("onEarlyMessage called with nil pub key")
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

// the very first step of handling a message.
func (proc *consensusProcess) handleMessage(ctx context.Context, m *Msg) {
	logger := proc.WithContext(ctx).WithFields(
		log.String("msg_type", m.InnerMsg.Type.String()),
		log.FieldNamed("sender_id", m.PubKey),
		log.Uint32("current_round", proc.getRound()),
		log.Uint32("msg_round", m.InnerMsg.Round),
		proc.layer)

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
	if err := proc.validator.ContextuallyValidateMessage(ctx, m, proc.getRound()); err != nil {
		// early message, keep for later
		if errors.Is(err, errEarlyMsg) {
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
		logger.With().Warning("late message failed contextual validation, discarding", log.Err(err))
		return
	}

	// validate syntax for contextually valid messages
	if !proc.validator.SyntacticallyValidateMessage(ctx, m) {
		logger.Warning("message failed syntactic validation, discarding")
		return
	}

	// warn on late pre-round msgs
	if m.InnerMsg.Type == pre && proc.getRound() != preRound {
		logger.Warning("encountered late preround message")
	}

	// valid, continue to process msg by type
	proc.processMsg(ctx, m)
}

// process the message by its type.
func (proc *consensusProcess) processMsg(ctx context.Context, m *Msg) {
	proc.WithContext(ctx).With().Debug("processing message",
		proc.layer,
		log.String("msg_type", m.InnerMsg.Type.String()),
		log.Int("num_values", len(m.InnerMsg.Values)))

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
			proc.layer,
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
		proc.layer,
		log.Uint32("msg_round", msg.InnerMsg.Round),
		log.String("msg_type", msg.InnerMsg.Type.String()),
		log.Int("eligibility_count", int(msg.InnerMsg.EligibilityCount)),
		log.String("current_set", proc.value.String()),
		log.Uint32("current_round", proc.getRound()),
	)
	logger := proc.WithContext(ctx)

	if err := proc.publisher.Publish(ctx, pubsub.HareProtocol, msg.Bytes()); err != nil {
		logger.With().Error("failed to broadcast round message", log.Err(err))
		return false
	}

	logger.Debug("should participate: message sent")
	return true
}

// logic of the end of a round by the round type.
func (proc *consensusProcess) onRoundEnd(ctx context.Context) {
	logger := proc.WithContext(ctx).WithFields(
		log.Uint32("current_round", proc.getRound()),
		proc.layer)
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
		logger.Event().Debug("proposal round ended",
			log.Int("set_size", proc.value.Size()),
			log.String("proposed_set", sStr),
			log.Bool("is_conflicting", proc.proposalTracker.IsConflicting()))
	case commitRound:
		logger.With().Debug("commit round ended", log.Int("set_size", proc.value.Size()))
	}
}

// advances the state to the next round.
func (proc *consensusProcess) advanceToNextRound(ctx context.Context) {
	newRound := proc.addToRound(1)
	if newRound >= RoundsPerIteration && newRound%RoundsPerIteration == 0 {
		proc.WithContext(ctx).Event().Warning("starting new iteration",
			log.Uint32("current_round", newRound),
			proc.layer)
	}
}

func (proc *consensusProcess) beginStatusRound(ctx context.Context) {
	proc.statusesTracker = newStatusTracker(proc.cfg.F+1, proc.cfg.N)
	proc.statusesTracker.Log = proc.Log

	// check participation
	if !proc.shouldParticipate(ctx) {
		return
	}

	b, err := proc.initDefaultBuilder(proc.value)
	if err != nil {
		proc.WithContext(ctx).With().Error("failed to init msg builder", proc.layer, log.Err(err))
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
			proc.WithContext(ctx).With().Error("failed to init msg builder", proc.layer, log.Err(err))
			return
		}
		svp := proc.statusesTracker.BuildSVP()
		if svp != nil {
			proposalMsg := builder.SetType(proposal).SetSVP(svp).Sign(proc.signing).Build()
			proc.sendMessage(ctx, proposalMsg)
		} else {
			proc.WithContext(ctx).With().Error("failed to build SVP", proc.layer)
		}
	}
}

func (proc *consensusProcess) beginCommitRound(ctx context.Context) {
	proposedSet := proc.proposalTracker.ProposedSet()

	// proposedSet may be nil, in such case the tracker will ignore Messages
	proc.commitTracker = newCommitTracker(proc.cfg.F+1, proc.cfg.N, proposedSet) // track commits for proposed set

	if proposedSet == nil {
		return
	}

	// check participation
	if !proc.shouldParticipate(ctx) {
		return
	}

	builder, err := proc.initDefaultBuilder(proposedSet)
	if err != nil {
		proc.WithContext(ctx).With().Error("failed to init msg builder", proc.layer, log.Err(err))
		return
	}
	builder = builder.SetType(commit).Sign(proc.signing)
	commitMsg := builder.Build()
	proc.sendMessage(ctx, commitMsg)
}

func (proc *consensusProcess) beginNotifyRound(ctx context.Context) {
	logger := proc.WithContext(ctx).WithFields(proc.layer)
	proc.notifyTracker = newNotifyTracker(proc.cfg.N)

	// release proposal & commit trackers
	defer func() {
		proc.commitTracker = nil
		proc.proposalTracker = nil
	}()

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
		logger.Error("failed to build certificate at begin notify round")
		return
	}

	s := proc.proposalTracker.ProposedSet()
	if s == nil {
		logger.Error("failed to get proposal set at begin notify round")
		return
	}

	// update set & matching certificate
	proc.value = s
	proc.certificate = cert

	// check participation
	if !proc.shouldParticipate(ctx) {
		return
	}

	// build & send notify message
	builder, err := proc.initDefaultBuilder(proc.value)
	if err != nil {
		logger.With().Error("failed to init msg builder", proc.layer, log.Err(err))
		return
	}

	builder = builder.SetType(notify).SetCertificate(proc.certificate).Sign(proc.signing)
	notifyMsg := builder.Build()
	logger.With().Debug("sending notify message", notifyMsg)
	proc.sendMessage(ctx, notifyMsg)
}

// passes all pending messages to the inbox of the process so they will be handled.
func (proc *consensusProcess) handlePending(pending map[string]*Msg) {
	for _, m := range pending {
		proc.inbox <- m
	}
}

// runs the logic of the beginning of a round by its type
// pending messages are passed for handling.
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
		proc.Fatal(fmt.Sprintf("current round out of bounds. Expected: 0-3, Found: %v", proc.currentRound()))
	}

	if len(proc.pending) == 0 { // no pending messages
		return
	}

	// handle pending messages
	pendingProcess := proc.pending
	proc.pending = make(map[string]*Msg, proc.cfg.N)
	proc.eg.Go(func() error {
		proc.handlePending(pendingProcess)
		return nil
	})
}

// init a new message builder with the current state (s, k, ki) for this instance.
func (proc *consensusProcess) initDefaultBuilder(s *Set) (*messageBuilder, error) {
	builder := newMessageBuilder().SetLayer(proc.layer)
	builder = builder.SetRoundCounter(proc.getRound()).SetCommittedRound(proc.committedRound).SetValues(s)
	proof, err := proc.oracle.Proof(context.TODO(), proc.layer, proc.getRound())
	if err != nil {
		return nil, fmt.Errorf("init default builder: %w", err)
	}
	builder.SetRoleProof(proof)

	proc.mu.RLock()
	builder.SetEligibilityCount(proc.eligibilityCount)
	proc.mu.RUnlock()

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
		proc.WithContext(ctx).With().Warning("received proposal message for processing in an invalid context",
			log.Uint32("current_round", proc.getRound()),
			log.Uint32("msg_round", msg.InnerMsg.Round))
	}
}

func (proc *consensusProcess) processCommitMsg(ctx context.Context, msg *Msg) {
	proc.mTracker.Track(msg) // a commit msg passed for processing is assumed to be valid
	proc.commitTracker.OnCommit(msg)
}

func (proc *consensusProcess) processNotifyMsg(ctx context.Context, msg *Msg) {
	if proc.notifyTracker == nil {
		proc.WithContext(ctx).Error("notify tracker is nil")
		return
	}

	s := NewSet(msg.InnerMsg.Values)

	if ignored := proc.notifyTracker.OnNotify(msg); ignored {
		proc.WithContext(ctx).With().Warning("ignoring notification",
			log.String("sender_id", msg.PubKey.ShortString()))
		return
	}

	if proc.currentRound() == notifyRound { // not necessary to update otherwise
		// we assume that this expression was checked before
		if msg.InnerMsg.Cert.AggMsgs.Messages[0].InnerMsg.Round >= proc.committedRound { // update state iff K >= Ki
			proc.value = s
			proc.certificate = msg.InnerMsg.Cert
			proc.committedRound = msg.InnerMsg.CommittedRound
		}
	}

	if proc.notifyTracker.NotificationsCount(s) < proc.cfg.F+1 { // not enough
		proc.WithContext(ctx).With().Debug("not enough notifications for termination",
			log.String("current_set", proc.value.String()),
			log.Uint32("current_round", proc.getRound()),
			proc.layer,
			log.Int("expected", proc.cfg.F+1),
			log.Int("actual", proc.notifyTracker.NotificationsCount(s)))
		return
	}

	// enough notifications, should terminate
	proc.value = s // update to the agreed set
	proc.WithContext(ctx).Event().Info("consensus process terminated",
		log.String("current_set", proc.value.String()),
		log.Uint32("current_round", proc.getRound()),
		proc.layer,
		log.Int("set_size", proc.value.Size()))
	proc.report(completed)
	numIterations.Observe(float64(proc.getRound()))
	proc.terminate()
}

func (proc *consensusProcess) currentRound() uint32 {
	return proc.getRound() % RoundsPerIteration
}

// returns a function to validate status messages.
func (proc *consensusProcess) statusValidator() func(m *Msg) bool {
	validate := func(m *Msg) bool {
		s := NewSet(m.InnerMsg.Values)
		if m.InnerMsg.CommittedRound == preRound { // no certificates, validate by pre-round msgs
			if proc.preRoundTracker.CanProveSet(s) { // can prove s
				return true
			}
		} else { // if CommittedRound (Ki) >= 0, we should have received a certificate for that set
			if proc.notifyTracker.HasCertificate(m.InnerMsg.CommittedRound, s) { // can prove s
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
	proc.Event().Debug("status round ended",
		log.Bool("is_svp_ready", proc.statusesTracker.IsSVPReady()),
		proc.layer,
		log.Int("set_size", proc.value.Size()),
		log.String("analyze_duration", time.Since(before).String()))
}

// checks if we should participate in the current round
// returns true if we should participate, false otherwise.
func (proc *consensusProcess) shouldParticipate(ctx context.Context) bool {
	logger := proc.WithContext(ctx).WithFields(
		log.Uint32("current_round", proc.getRound()),
		proc.layer)

	// query if identity is active
	nid := types.BytesToNodeID(proc.signing.PublicKey().Bytes())
	res, err := proc.oracle.IsIdentityActiveOnConsensusView(ctx, nid, proc.layer)
	if err != nil {
		logger.With().Error("failed to check own identity for activeness", log.Err(err))
		return false
	}

	if !res {
		logger.Debug("should not participate: identity is not active")
		return false
	}

	currentRole := proc.currentRole(ctx)
	if currentRole == passive {
		logger.Debug("should not participate: passive")
		return false
	}

	proc.mu.RLock()
	eligibilityCount := proc.eligibilityCount
	proc.mu.RUnlock()

	// should participate
	logger.With().Debug("should participate",
		log.Bool("leader", currentRole == leader),
		log.Uint32("eligibility_count", uint32(eligibilityCount)),
	)
	return true
}

// Returns the role matching the current round if eligible for this round, false otherwise.
func (proc *consensusProcess) currentRole(ctx context.Context) role {
	logger := proc.WithContext(ctx).WithFields(proc.layer)
	proof, err := proc.oracle.Proof(ctx, proc.layer, proc.getRound())
	if err != nil {
		logger.With().Error("failed to get eligibility proof from oracle", log.Err(err))
		return passive
	}

	k := proc.getRound()

	eligibilityCount, err := proc.oracle.CalcEligibility(ctx, proc.layer,
		k, expectedCommitteeSize(k, proc.cfg.N, proc.cfg.ExpectedLeaders), proc.nid, proof)
	if err != nil {
		logger.With().Error("failed to check eligibility", log.Err(err))
		return passive
	}

	proc.mu.Lock()
	proc.eligibilityCount = eligibilityCount
	proc.mu.Unlock()

	if eligibilityCount > 0 { // eligible
		if proc.currentRound() == proposalRound {
			return leader
		}
		return active
	}

	return passive
}

func (proc *consensusProcess) getRound() uint32 {
	return atomic.LoadUint32(&proc.round)
}

func (proc *consensusProcess) setRound(value uint32) {
	atomic.StoreUint32(&proc.round, value)
}

func (proc *consensusProcess) addToRound(value uint32) (new uint32) {
	return atomic.AddUint32(&proc.round, value)
}

// Returns the expected committee size for the given round assuming maxExpActives is the default size.
func expectedCommitteeSize(round uint32, maxExpActive, expLeaders int) int {
	if round%4 == proposalRound {
		return expLeaders // expected number of leaders
	}

	// N actives in any other case
	return maxExpActive
}
