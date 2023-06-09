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

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics"
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

const (
	completed    = true
	notCompleted = false
)

// report is the termination report of the CP.
type report struct {
	id        types.LayerID // layer id
	set       *Set          // agreed-upon set
	completed bool          // whether the CP completed
}

func (proc *consensusProcess) report(completed bool) {
	proc.comm.report <- report{id: proc.layer, set: proc.value, completed: completed}
}

type wcReport struct {
	id       types.LayerID
	coinflip bool
}

func (proc *consensusProcess) reportWeakCoin() {
	proc.comm.wc <- wcReport{id: proc.layer, coinflip: proc.preRoundTracker.coinflip}
}

// State holds the current state of the consensus process (aka the participant).
type State struct {
	round          uint32       // the round counter (round%4 is the round number); it should be first in struct for alignment because atomics are used (K)
	committedRound uint32       // indicates when value (S) was first committed upon (Ki)
	value          *Set         // the set of values (S)
	certificate    *Certificate // the certificate
}

// Upon receiving a protocol message, we try to build the full message.
// The full message consists of the original message and the extracted public key.
// An extracted public key is considered valid if it represents an active identity for a consensus view.
func checkIdentity(ctx context.Context, logger log.Log, hareMsg *Message, querier stateQuerier) error {
	logger = logger.WithContext(ctx)

	// query if identity is active
	res, err := querier.IsIdentityActiveOnConsensusView(ctx, hareMsg.SmesherID, hareMsg.Layer)
	if err != nil {
		logger.With().Error("failed to check if identity is active",
			log.Stringer("smesher", hareMsg.SmesherID),
			log.Err(err),
			hareMsg.Layer,
			log.String("msg_type", hareMsg.Type.String()),
		)
		return fmt.Errorf("check active identity: %w", err)
	}

	// check query result
	if !res {
		logger.With().Warning("identity is not active",
			log.Stringer("smesher", hareMsg.SmesherID),
			hareMsg.Layer,
			log.String("msg_type", hareMsg.Type.String()),
		)
		return errors.New("inactive identity")
	}

	return nil
}

type CountInfo struct {
	// number of nodes that are honest, dishonest and known equivocators.
	// known equivocators are those the node only has eligibility proof but no hare message.
	numHonest, numDishonest, numKE int
	hCount, dhCount, keCount       int
}

func (ci *CountInfo) Add(other *CountInfo) *CountInfo {
	return &CountInfo{
		numHonest:    ci.numHonest + other.numHonest,
		numDishonest: ci.numDishonest + other.numDishonest,
		numKE:        ci.numKE + other.numKE,
		hCount:       ci.hCount + other.hCount,
		dhCount:      ci.dhCount + other.dhCount,
		keCount:      ci.keCount + other.keCount,
	}
}

func (ci *CountInfo) IncHonest(count uint16) {
	ci.numHonest++
	ci.hCount += int(count)
}

func (ci *CountInfo) IncDishonest(count uint16) {
	ci.numDishonest++
	ci.dhCount += int(count)
}

func (ci *CountInfo) IncKnownEquivocator(count uint16) {
	ci.numKE++
	ci.keCount += int(count)
}

func (ci *CountInfo) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddInt("honest_count", ci.hCount)
	encoder.AddInt("num_honest", ci.numHonest)
	encoder.AddInt("dishonest_count", ci.dhCount)
	encoder.AddInt("num_dishonest", ci.numDishonest)
	encoder.AddInt("num_ke", ci.numKE)
	encoder.AddInt("ke_count", ci.keCount)
	return nil
}

func (ci *CountInfo) Meet(threshold int) bool {
	// at least one honest party with good message
	return ci.hCount > 0 && ci.hCount+ci.dhCount+ci.keCount >= threshold
}

type communication struct {
	// broker sends all gossip messages to inbox.
	inbox chan any
	// consensus process sends all malfeasance discovered during hare process
	// to this mchOut
	mchOut chan<- *types.MalfeasanceGossip
	// if the consensus process terminates, output the result to report
	report chan report
	wc     chan wcReport
}

// consensusProcess is an entity (a single participant) in the Hare protocol.
// Once started, the CP iterates through the rounds until consensus is reached or the instance is canceled.
// The output is then written to the provided TerminationReport channel.
// If the consensus process is canceled one should not expect the output to be written to the output channel.
type consensusProcess struct {
	log.Log
	State

	ctx              context.Context
	cancel           context.CancelFunc
	eg               errgroup.Group
	mu               sync.RWMutex
	layer            types.LayerID
	oracle           Rolacle // the roles oracle provider
	signer           *signing.EdSigner
	nid              types.NodeID
	nonce            *types.VRFPostIndex
	publisher        pubsub.Publisher
	comm             communication
	validator        messageValidator
	preRoundTracker  *preRoundTracker
	statusesTracker  *statusTracker
	proposalTracker  proposalTrackerProvider
	commitTracker    commitTrackerProvider
	notifyTracker    *notifyTracker
	cfg              config.Config
	pending          map[types.NodeID]*Message // buffer for early messages that are pending process
	mTracker         *msgsTracker              // tracks valid messages
	eTracker         *EligibilityTracker       // tracks eligible identities by rounds
	eligibilityCount uint16
	clock            RoundClock
	once             sync.Once
}

// newConsensusProcess creates a new consensus process instance.
func newConsensusProcess(
	ctx context.Context,
	cfg config.Config,
	layer types.LayerID,
	s *Set,
	oracle Rolacle,
	stateQuerier stateQuerier,
	signing *signing.EdSigner,
	edVerifier *signing.EdVerifier,
	nid types.NodeID,
	nonce *types.VRFPostIndex,
	p2p pubsub.Publisher,
	comm communication,
	ev roleValidator,
	clock RoundClock,
	logger log.Log,
) *consensusProcess {
	proc := &consensusProcess{
		State: State{
			round:          preRound,
			committedRound: preRound,
			value:          s.Clone(),
		},
		layer:     layer,
		oracle:    oracle,
		signer:    signing,
		nid:       nid,
		nonce:     nonce,
		publisher: p2p,
		cfg:       cfg,
		comm:      comm,
		pending:   make(map[types.NodeID]*Message, cfg.N),
		Log:       logger,
		mTracker:  newMsgsTracker(),
		eTracker:  NewEligibilityTracker(cfg.N),
		clock:     clock,
	}
	proc.ctx, proc.cancel = context.WithCancel(ctx)
	proc.preRoundTracker = newPreRoundTracker(logger.WithContext(proc.ctx).WithFields(proc.layer), comm.mchOut, proc.eTracker, cfg.N/2+1, cfg.N)
	proc.validator = newSyntaxContextValidator(signing, edVerifier, cfg.N/2+1, proc.statusValidator(), stateQuerier, ev, proc.mTracker, proc.eTracker, logger)

	return proc
}

// Returns the iteration number from a given round counter.
func inferIteration(roundCounter uint32) uint32 {
	return roundCounter / RoundsPerIteration
}

// Start the consensus process.
// It starts the PreRound round and then iterates through the rounds until consensus is reached or the instance is canceled.
// It is assumed that the inbox is set before the call to Start.
// It returns an error if Start has been called more than once or the inbox is nil.
func (proc *consensusProcess) Start() {
	proc.once.Do(func() {
		proc.eg.Go(func() error {
			proc.eventLoop()
			return nil
		})
	})
}

// ID returns the instance id.
func (proc *consensusProcess) ID() types.LayerID {
	return proc.layer
}

func (proc *consensusProcess) terminate() {
	proc.cancel()
}

func (proc *consensusProcess) Stop() {
	_ = proc.eg.Wait()
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
		log.Stringer("current_set", proc.value),
		log.Int("set_size", proc.value.Size()),
	)

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
			m := builder.SetType(pre).Sign(proc.signer).Build()
			proc.sendMessage(ctx, m)
		} else {
			logger.With().Debug("should not participate",
				log.Uint32("current_round", proc.getRound()),
			)
		}
		return nil
	})

	endOfRound := proc.clock.AwaitEndOfRound(preRound)

PreRound:
	for {
		select {
		// listen to pre-round Messages
		case msg := <-proc.comm.inbox:
			if hmsg, ok := msg.(*Message); ok {
				proc.handleMessage(ctx, hmsg)
			} else if emsg, ok := msg.(*types.HareEligibilityGossip); ok {
				proc.onMalfeasance(emsg)
			} else {
				proc.Log.Fatal("unexpected message type")
			}
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
		logger.With().Info("preround ended",
			log.Int("set_size", proc.value.Size()))
	}
	proc.reportWeakCoin()
	proc.advanceToNextRound(ctx) // K was initialized to -1, K should be 0

	// start first iteration
	proc.onRoundBegin(ctx)
	endOfRound = proc.clock.AwaitEndOfRound(proc.getRound())

	for {
		select {
		case msg := <-proc.comm.inbox: // msg event
			if proc.terminating() {
				return
			}
			if hmsg, ok := msg.(*Message); ok {
				proc.handleMessage(ctx, hmsg)
			} else if emsg, ok := msg.(*types.HareEligibilityGossip); ok {
				proc.onMalfeasance(emsg)
			} else {
				proc.Log.Fatal("unexpected message type")
			}
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
				return
			}
			proc.onRoundBegin(ctx)
			endOfRound = proc.clock.AwaitEndOfRound(round)

		case <-proc.ctx.Done(): // close event
			logger.With().Debug("terminating: received signal",
				log.Uint32("current_round", proc.getRound()))
			return
		}
	}
}

// handles eligibility proof from hare gossip handler and malfeasance proof gossip handler.
func (proc *consensusProcess) onMalfeasance(msg *types.HareEligibilityGossip) {
	proc.eTracker.Track(msg.NodeID, msg.Round, msg.Eligibility.Count, false)
}

// handles a message that has arrived early.
func (proc *consensusProcess) onEarlyMessage(ctx context.Context, m *Message) {
	logger := proc.WithContext(ctx)

	if m == nil {
		logger.Error("onEarlyMessage called with nil")
		return
	}

	if m.InnerMessage == nil {
		logger.Error("onEarlyMessage called with nil inner message")
		return
	}

	if m.SmesherID == types.EmptyNodeID {
		logger.Error("onEarlyMessage called with nil pub key")
		return
	}

	if _, exist := proc.pending[m.SmesherID]; exist { // ignore, already received
		logger.With().Warning("already received message from sender",
			log.Stringer("smesher", m.SmesherID),
		)
		return
	}

	proc.pending[m.SmesherID] = m
}

// the very first step of handling a message.
func (proc *consensusProcess) handleMessage(ctx context.Context, m *Message) {
	logger := proc.WithContext(ctx).WithFields(
		log.String("msg_type", m.Type.String()),
		log.Stringer("smesher", m.SmesherID),
		log.Uint32("current_round", proc.getRound()),
		log.Uint32("msg_round", m.Round),
		proc.layer,
	)

	// Note: instanceID is already verified by the broker
	logger.Debug("consensus process received message")
	// broker already validated the eligibility of this message
	proc.eTracker.Track(m.SmesherID, m.Round, m.Eligibility.Count, true)

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
	if m.Type == pre && proc.getRound() != preRound {
		logger.Warning("encountered late preround message")
	}

	// valid, continue to process msg by type
	proc.processMsg(ctx, m)
}

// process the message by its type.
func (proc *consensusProcess) processMsg(ctx context.Context, m *Message) {
	proc.WithContext(ctx).With().Debug("processing message",
		proc.layer,
		log.String("msg_type", m.Type.String()),
		log.Int("num_values", len(m.Values)),
	)

	// Report the latency since the beginning of the round
	latency := time.Since(proc.clock.RoundEnd(m.Round - 1))
	metrics.ReportMessageLatency(pubsub.HareProtocol, m.Type.String(), latency)
	switch m.Type {
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
			log.String("msg_type", m.Type.String()),
			log.Stringer("smesher", m.SmesherID),
		)
	}
}

// sends a message to the network.
// Returns true if the message is assumed to be sent, false otherwise.
func (proc *consensusProcess) sendMessage(ctx context.Context, msg *Message) bool {
	// invalid msg
	if msg == nil {
		proc.WithContext(ctx).Error("sendMessage was called with nil")
		return false
	}

	// generate a new requestID for this message
	ctx = log.WithNewRequestID(ctx,
		proc.layer,
		log.Uint32("msg_round", msg.Round),
		log.String("msg_type", msg.Type.String()),
		log.Int("eligibility_count", int(msg.Eligibility.Count)),
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
			log.Uint32("iteration", inferIteration(newRound)),
			log.Uint32("current_round", newRound),
			proc.layer)
	}
}

func (proc *consensusProcess) beginStatusRound(ctx context.Context) {
	proc.statusesTracker = newStatusTracker(
		proc.Log.WithContext(proc.ctx).WithFields(proc.layer),
		proc.getRound(),
		proc.comm.mchOut,
		proc.eTracker,
		proc.cfg.N/2+1,
		proc.cfg.N)

	// check participation
	if !proc.shouldParticipate(ctx) {
		return
	}

	b, err := proc.initDefaultBuilder(proc.value)
	if err != nil {
		proc.WithContext(ctx).With().Error("failed to init msg builder", proc.layer, log.Err(err))
		return
	}
	statusMsg := b.SetType(status).Sign(proc.signer).Build()
	proc.sendMessage(ctx, statusMsg)
}

func (proc *consensusProcess) beginProposalRound(ctx context.Context) {
	proc.proposalTracker = newProposalTracker(
		proc.Log.WithContext(proc.ctx).WithFields(proc.layer),
		proc.comm.mchOut,
		proc.eTracker)

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
			proposalMsg := builder.SetType(proposal).SetSVP(svp).Sign(proc.signer).Build()
			proc.sendMessage(ctx, proposalMsg)
		} else {
			proc.WithContext(ctx).With().Error("failed to build SVP", proc.layer)
		}
	}
}

func (proc *consensusProcess) beginCommitRound(ctx context.Context) {
	proposedSet := proc.proposalTracker.ProposedSet()

	// proposedSet may be nil, in such case the tracker will ignore Messages
	proc.commitTracker = newCommitTracker(
		proc.Log.WithContext(proc.ctx).WithFields(proc.layer),
		proc.getRound(),
		proc.comm.mchOut,
		proc.eTracker,
		proc.cfg.N/2+1,
		proc.cfg.N,
		proposedSet)

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
	builder = builder.SetType(commit).Sign(proc.signer)
	commitMsg := builder.Build()
	proc.sendMessage(ctx, commitMsg)
}

func (proc *consensusProcess) beginNotifyRound(ctx context.Context) {
	logger := proc.WithContext(ctx).WithFields(proc.layer)
	proc.notifyTracker = newNotifyTracker(
		proc.Log.WithContext(proc.ctx).WithFields(proc.layer),
		proc.getRound(),
		proc.comm.mchOut,
		proc.eTracker,
		proc.cfg.N,
	)

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
			log.Int("expected", proc.cfg.N/2+1),
			log.Object("actual", proc.commitTracker.CommitCount()))
		return
	}

	cert := proc.commitTracker.BuildCertificate()
	if cert == nil {
		logger.Error("failed to build certificate at begin notify round")
		return
	}

	s := proc.proposalTracker.ProposedSet()
	if s == nil {
		// it's possible we received a late conflicting proposal
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

	builder = builder.SetType(notify).SetCertificate(proc.certificate).Sign(proc.signer)
	notifyMsg := builder.Build()
	logger.With().Debug("sending notify message", notifyMsg)
	proc.sendMessage(ctx, notifyMsg)
}

// passes all pending messages to the inbox of the process so they will be handled.
func (proc *consensusProcess) handlePending(pending map[types.NodeID]*Message) {
	for _, m := range pending {
		select {
		case <-proc.ctx.Done():
			return
		case proc.comm.inbox <- m:
		}
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
	proc.pending = make(map[types.NodeID]*Message, proc.cfg.N)
	proc.eg.Go(func() error {
		proc.handlePending(pendingProcess)
		return nil
	})
}

// init a new message builder with the current state (s, k, ki) for this instance.
func (proc *consensusProcess) initDefaultBuilder(s *Set) (*messageBuilder, error) {
	if proc.nonce == nil {
		proc.Log.Fatal("initDefaultBuilder: missing vrf nonce")
	}
	builder := newMessageBuilder().SetLayer(proc.layer)
	builder = builder.SetRoundCounter(proc.getRound()).SetCommittedRound(proc.committedRound).SetValues(s)
	proof, err := proc.oracle.Proof(context.TODO(), *proc.nonce, proc.layer, proc.getRound())
	if err != nil {
		return nil, fmt.Errorf("init default builder: %w", err)
	}
	builder.SetRoleProof(proof)
	builder.SetEligibilityCount(proc.getEligibilityCount())
	return builder, nil
}

func (proc *consensusProcess) processPreRoundMsg(ctx context.Context, msg *Message) {
	proc.preRoundTracker.OnPreRound(ctx, msg)
}

func (proc *consensusProcess) processStatusMsg(ctx context.Context, msg *Message) {
	// record status
	proc.statusesTracker.RecordStatus(ctx, msg)
}

func (proc *consensusProcess) processProposalMsg(ctx context.Context, msg *Message) {
	currRnd := proc.currentRound()

	if currRnd == proposalRound { // regular proposal
		proc.proposalTracker.OnProposal(ctx, msg)
	} else if currRnd == commitRound { // late proposal
		proc.proposalTracker.OnLateProposal(ctx, msg)
	} else {
		proc.WithContext(ctx).With().Warning("received proposal message for processing in an invalid context",
			log.Uint32("current_round", proc.getRound()),
			log.Uint32("msg_round", msg.Round))
	}
}

func (proc *consensusProcess) processCommitMsg(ctx context.Context, msg *Message) {
	proc.mTracker.Track(msg) // a commit msg passed for processing is assumed to be valid
	proc.commitTracker.OnCommit(ctx, msg)
}

func (proc *consensusProcess) processNotifyMsg(ctx context.Context, msg *Message) {
	if proc.notifyTracker == nil {
		proc.WithContext(ctx).Error("notify tracker is nil")
		return
	}

	s := NewSet(msg.Values)

	if ignored := proc.notifyTracker.OnNotify(ctx, msg); ignored {
		proc.WithContext(ctx).With().Warning("ignoring notification",
			log.Stringer("smesher", msg.SmesherID),
		)
		return
	}

	if proc.currentRound() == notifyRound { // not necessary to update otherwise
		// we assume that this expression was checked before
		if msg.Cert.AggMsgs.Messages[0].Round >= proc.committedRound { // update state iff K >= Ki
			proc.value = s
			proc.certificate = msg.Cert
			proc.committedRound = msg.CommittedRound
		}
	}

	threshold := proc.cfg.N/2 + 1
	notifyCount := proc.notifyTracker.NotificationsCount(s)
	if notifyCount == nil {
		proc.WithContext(ctx).Fatal("unexpected count")
	}
	if !notifyCount.Meet(threshold) { // not enough
		proc.WithContext(ctx).With().Debug("not enough notifications for termination",
			log.String("current_set", proc.value.String()),
			log.Uint32("current_round", proc.getRound()),
			proc.layer,
			log.Int("expected", threshold),
			log.Object("actual", notifyCount))
		return
	}

	// enough notifications, should terminate
	proc.value = s // update to the agreed set
	proc.WithContext(ctx).Event().Info("consensus process terminated",
		log.String("current_set", proc.value.String()),
		log.Uint32("current_round", proc.getRound()),
		proc.layer,
		log.Object("notify_count", notifyCount),
		log.Int("set_size", proc.value.Size()))
	proc.report(completed)
	numIterations.Observe(float64(proc.getRound()))
	proc.terminate()
}

func (proc *consensusProcess) currentRound() uint32 {
	return proc.getRound() % RoundsPerIteration
}

// returns a function to validate status messages.
func (proc *consensusProcess) statusValidator() func(m *Message) bool {
	validate := func(m *Message) bool {
		s := NewSet(m.Values)
		if m.CommittedRound == preRound { // no certificates, validate by pre-round msgs
			if proc.preRoundTracker.CanProveSet(s) { // can prove s
				return true
			}
		} else if proc.notifyTracker.HasCertificate(m.CommittedRound, s) { // can prove s
			// if CommittedRound (Ki) >= 0, we should have received a certificate for that set
			return true
		}
		return false
	}

	return validate
}

func (proc *consensusProcess) endOfStatusRound() {
	// validate and track wrapper for validation func
	valid := proc.statusValidator()
	vtFunc := func(m *Message) bool {
		if valid(m) {
			proc.mTracker.Track(m)
			return true
		}

		return false
	}

	// assumption: AnalyzeStatusMessages calls vtFunc for every recorded status message
	before := time.Now()
	proc.statusesTracker.AnalyzeStatusMessages(vtFunc)
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

	if proc.nonce == nil {
		logger.Debug("should not participate: identity missing vrf nonce")
		return false
	}

	// query if identity is active
	res, err := proc.oracle.IsIdentityActiveOnConsensusView(ctx, proc.signer.NodeID(), proc.layer)
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

	eligibilityCount := proc.getEligibilityCount()

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
	if proc.nonce == nil {
		logger.Fatal("currentRole: missing vrf nonce")
	}
	proof, err := proc.oracle.Proof(ctx, *proc.nonce, proc.layer, proc.getRound())
	if err != nil {
		logger.With().Error("failed to get eligibility proof from oracle", log.Err(err))
		return passive
	}

	k := proc.getRound()

	size := expectedCommitteeSize(k, proc.cfg.N, proc.cfg.ExpectedLeaders)
	eligibilityCount, err := proc.oracle.CalcEligibility(ctx, proc.layer, k, size, proc.nid, *proc.nonce, proof)
	if err != nil {
		logger.With().Error("failed to check eligibility", log.Err(err))
		return passive
	}

	proc.setEligibilityCount(eligibilityCount)

	if eligibilityCount > 0 { // eligible
		if proc.currentRound() == proposalRound {
			return leader
		}
		return active
	}

	return passive
}

func (proc *consensusProcess) getEligibilityCount() uint16 {
	proc.mu.RLock()
	defer proc.mu.RUnlock()
	return proc.eligibilityCount
}

func (proc *consensusProcess) setEligibilityCount(count uint16) {
	proc.mu.Lock()
	defer proc.mu.Unlock()
	proc.eligibilityCount = count
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
	if round%RoundsPerIteration == proposalRound {
		return expLeaders // expected number of leaders
	}

	// N actives in any other case
	return maxExpActive
}
