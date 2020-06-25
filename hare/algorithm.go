// Package hare implements the Hare Protocol.
package hare

import (
	"bytes"
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
	Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeID, sig []byte) (bool, error)
	Proof(layer types.LayerID, round int32) ([]byte, error)
	IsIdentityActiveOnConsensusView(edID string, layer types.LayerID) (bool, error)
}

// NetworkService provides the registration and broadcast abilities in the network.
type NetworkService interface {
	RegisterGossipProtocol(protocol string, prio priorityq.Priority) chan service.GossipMessage
	Broadcast(protocol string, payload []byte) error
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
	PubKey *signing.PublicKey
}

func (m *Msg) String() string {
	return fmt.Sprintf("Pubkey: %v Message: %v", m.PubKey.ShortString(), m.Message.String())
}

// Bytes returns the message as bytes (without the public key).
// It panics if the message erred on unmarshal.
func (m *Msg) Bytes() []byte {
	var w bytes.Buffer
	_, err := xdr.Marshal(&w, m.Message)
	if err != nil {
		log.Panic("could not marshal InnerMsg before send")
	}

	return w.Bytes()
}

// Upon receiving a protocol's message, we try to build the full message.
// The full message consists of the original message and the extracted public key.
// An extracted public key is considered valid if it represents an active identity for a consensus view.
func newMsg(hareMsg *Message, querier StateQuerier) (*Msg, error) {
	// extract pub key
	pubKey, err := ed25519.ExtractPublicKey(hareMsg.InnerMsg.Bytes(), hareMsg.Sig)
	if err != nil {
		log.With().Error("newMsg construction failed: could not extract public key", log.Err(err), log.Int("sig len", len(hareMsg.Sig)))
		return nil, err
	}
	// query if identity is active
	pub := signing.NewPublicKey(pubKey)
	res, err := querier.IsIdentityActiveOnConsensusView(pub.String(), types.LayerID(hareMsg.InnerMsg.InstanceID))
	if err != nil {
		log.With().Error("error while checking if identity is active", log.String("sender_id", pub.ShortString()),
			log.Err(err), types.LayerID(hareMsg.InnerMsg.InstanceID), log.String("msg_type", hareMsg.InnerMsg.Type.String()))
		return nil, errors.New("is identity active query failed")
	}

	// check query result
	if !res {
		log.With().Error("identity is not active", log.String("sender_id", pub.ShortString()),
			types.LayerID(hareMsg.InnerMsg.InstanceID), log.String("msg_type", hareMsg.InnerMsg.Type.String()))
		return nil, errors.New("inactive identity")
	}

	return &Msg{hareMsg, pub}, nil
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
		preRoundTracker:   newPreRoundTracker(cfg.F+1, cfg.N),
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
func (proc *consensusProcess) Start() error {
	if proc.isStarted { // called twice on same instance
		proc.Error("consensusProcess has already been started")
		return startInstanceError(errors.New("instance already started"))
	}

	if proc.s.Size() == 0 { // empty set is not valid
		proc.Error("consensusProcess cannot be started with an empty set")
		return startInstanceError(errors.New("instance started with an empty set"))
	}

	if proc.inbox == nil { // no inbox
		proc.Error("consensusProcess cannot be started with nil inbox")
		return startInstanceError(errors.New("instance started with nil inbox"))
	}

	proc.isStarted = true

	go proc.eventLoop()

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
func (proc *consensusProcess) eventLoop() {
	proc.With().Info("Consensus Process Started",
		log.Int("Hare-N", proc.cfg.N), log.Int("f", proc.cfg.F), log.String("duration", (time.Duration(proc.cfg.RoundDuration)*time.Second).String()),
		types.LayerID(proc.instanceID), log.Int("exp_leaders", proc.cfg.ExpectedLeaders), log.String("current_set", proc.s.String()), log.Int("set_size", proc.s.Size()))

	// start the timer
	timer := time.NewTimer(time.Duration(proc.cfg.RoundDuration) * time.Second)

	// check participation and send message
	go func() {
		// check participation
		if proc.shouldParticipate() {
			// set pre-round InnerMsg and send
			builder, err := proc.initDefaultBuilder(proc.s)
			if err != nil {
				proc.Error("init default builder failed: %v", err)
				return
			}
			m := builder.SetType(pre).Sign(proc.signing).Build()
			proc.sendMessage(m)
		}
	}()

PreRound:
	for {
		select {
		// listen to pre-round Messages
		case msg := <-proc.inbox:
			proc.handleMessage(msg)
		case <-timer.C:
			break PreRound
		case <-proc.CloseChannel():
			return
		}
	}
	proc.preRoundTracker.FilterSet(proc.s)
	if proc.s.Size() == 0 {
		proc.Event().Error("Fatal: PreRound ended with empty set", types.LayerID(proc.instanceID))
	} else {
		proc.Info("PreRound ended")
	}
	proc.advanceToNextRound() // K was initialized to -1, K should be 0

	// start first iteration
	proc.onRoundBegin()
	ticker := time.NewTicker(time.Duration(proc.cfg.RoundDuration) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg := <-proc.inbox: // msg event
			proc.handleMessage(msg)
			if proc.terminating {
				return
			}
		case <-ticker.C: // next round event
			proc.onRoundEnd()
			proc.advanceToNextRound()

			// exit if we reached the limit on number of iterations
			if proc.k/4 >= int32(proc.cfg.LimitIterations) {
				proc.Warning("terminating: reached iterations limit")
				proc.report(notCompleted)
				return
			}

			proc.onRoundBegin()
		case <-proc.CloseChannel(): // close event
			proc.Info("terminating: received termination signal")
			proc.report(notCompleted)
			return
		}
	}
}

// handles a message that has arrived early
func (proc *consensusProcess) onEarlyMessage(m *Msg) {
	if m == nil {
		proc.Error("onEarlyMessage called with nil")
		return
	}

	if m.Message == nil {
		proc.Error("onEarlyMessage called with nil message")
		return
	}

	if m.InnerMsg == nil {
		proc.Error("onEarlyMessage called with nil inner message")
		return
	}

	pub := m.PubKey
	if _, exist := proc.pending[pub.String()]; exist { // ignore, already received
		proc.Warning("Already received message from sender %v", pub.ShortString())
		return
	}

	proc.pending[pub.String()] = m
}

// the very first step of handling a message
func (proc *consensusProcess) handleMessage(m *Msg) {
	// Note: instanceID is already verified by the broker

	proc.With().Debug("Received message", log.String("msg_type", m.InnerMsg.Type.String()))

	// validate context
	err := proc.validator.ContextuallyValidateMessage(m, proc.k)
	if err != nil {
		mType := m.InnerMsg.Type.String()

		// early message, keep for later
		if err == errEarlyMsg {
			proc.With().Debug("Early message detected, keeping",
				log.String("msg_type", mType),
				log.String("sender_id", m.PubKey.ShortString()),
				log.Int32("current_k", proc.k),
				log.Int32("msg_k", m.InnerMsg.K),
				types.LayerID(proc.instanceID),
				log.Err(err))

			// validate syntax for early messages
			if !proc.validator.SyntacticallyValidateMessage(m) {
				proc.With().Warning("Early message failed syntactic validation",
					log.String("msg_type", mType),
					log.String("sender_id", m.PubKey.ShortString()))
				return
			}

			proc.onEarlyMessage(m)
			return
		}

		// not an early message but also contextually invalid
		proc.With().Error("Late message failed contextual validation",
			log.String("msg_type", mType),
			log.String("sender_id", m.PubKey.ShortString()),
			log.Int32("current_k", proc.k),
			log.Int32("msg_k", m.InnerMsg.K),
			types.LayerID(proc.instanceID), log.Err(err))
		return
	}

	// validate syntax for contextually valid messages
	if !proc.validator.SyntacticallyValidateMessage(m) {
		proc.Warning("Syntactically validation failed, pubkey %v", m.PubKey.ShortString())
		return
	}

	// warn on late pre-round msgs
	if m.InnerMsg.Type == pre && proc.k != -1 {
		proc.With().Warning("encountered late PreRound message", log.String("sender_id", m.PubKey.ShortString()), types.LayerID(proc.instanceID))
	}

	// valid, continue process msg by type
	proc.processMsg(m)
}

// process the message by its type
func (proc *consensusProcess) processMsg(m *Msg) {
	proc.Debug("Processing message of type %v", m.InnerMsg.Type.String())
	// TODO: fix metrics
	// metrics.MessageTypeCounter.With("type_id", m.InnerMsg.Type.String(), "layer", strconv.FormatUint(uint64(m.InnerMsg.InstanceID), 10), "reporter", "processMsg").Add(1)

	switch m.InnerMsg.Type {
	case pre:
		proc.processPreRoundMsg(m)
	case status: // end of round 1
		proc.processStatusMsg(m)
	case proposal: // end of round 2
		proc.processProposalMsg(m)
	case commit: // end of round 3
		proc.processCommitMsg(m)
	case notify: // end of round 4
		proc.processNotifyMsg(m)
	default:
		proc.Warning("Unknown message type: %v , pubkey %v", m.InnerMsg.Type, m.PubKey.ShortString())
	}
}

// sends a message to the network.
// Returns true if the message is assumed to be sent, false otherwise.
func (proc *consensusProcess) sendMessage(msg *Msg) bool {
	// invalid msg
	if msg == nil {
		proc.Error("sendMessage was called with nil")
		return false
	}

	if err := proc.network.Broadcast(protoName, msg.Bytes()); err != nil {
		proc.Error("Could not broadcast round message ", err.Error())
		return false
	}

	proc.With().Info("message sent",
		log.String("current_set", proc.s.String()),
		log.String("msg_type", msg.InnerMsg.Type.String()),
		types.LayerID(proc.instanceID))
	return true
}

// logic of the end of a round by the round type
func (proc *consensusProcess) onRoundEnd() {
	proc.With().Debug("End of round", log.Int32("K", proc.k), types.LayerID(proc.instanceID))

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
		proc.Event().Info("proposal round ended",
			log.String("proposed_set", sStr),
			log.Bool("is_conflicting", proc.proposalTracker.IsConflicting()),
			types.LayerID(proc.instanceID))
	case commitRound:
		proc.With().Info("commit round ended", types.LayerID(proc.instanceID))
	}
}

// advances the state to the next round
func (proc *consensusProcess) advanceToNextRound() {
	proc.k++
	if proc.k >= 4 && proc.k%4 == 0 {
		proc.Event().Warning("Starting new iteration", log.Int32("round_counter", proc.k),
			types.LayerID(proc.instanceID))
	}
}

func (proc *consensusProcess) beginStatusRound() {
	proc.statusesTracker = newStatusTracker(proc.cfg.F+1, proc.cfg.N)
	proc.statusesTracker.Log = proc.Log

	// check participation
	if !proc.shouldParticipate() {
		return
	}

	b, err := proc.initDefaultBuilder(proc.s)
	if err != nil {
		proc.Error("init default builder failed: %v", err)
		return
	}
	statusMsg := b.SetType(status).Sign(proc.signing).Build()
	proc.sendMessage(statusMsg)
}

func (proc *consensusProcess) beginProposalRound() {
	proc.proposalTracker = newProposalTracker(proc.Log)

	// done with building proposal, reset statuses tracking
	defer func() { proc.statusesTracker = nil }()

	if proc.statusesTracker.IsSVPReady() && proc.shouldParticipate() {
		builder, err := proc.initDefaultBuilder(proc.statusesTracker.ProposalSet(defaultSetSize))
		if err != nil {
			proc.Error("init default builder failed: %v", err)
			return
		}
		svp := proc.statusesTracker.BuildSVP()
		if svp != nil {
			proposalMsg := builder.SetType(proposal).SetSVP(svp).Sign(proc.signing).Build()
			proc.sendMessage(proposalMsg)
		} else {
			proc.Error("Failed to build SVP (nil) after verifying SVP is ready ")
		}
	}
}

func (proc *consensusProcess) beginCommitRound() {
	proposedSet := proc.proposalTracker.ProposedSet()

	// proposedSet may be nil, in such case the tracker will ignore Messages
	proc.commitTracker = newCommitTracker(proc.cfg.F+1, proc.cfg.N, proposedSet) // track commits for proposed set

	if proposedSet != nil { // has proposal to commit on

		// check participation
		if !proc.shouldParticipate() {
			return
		}

		builder, err := proc.initDefaultBuilder(proposedSet)
		if err != nil {
			proc.Error("init default builder failed: %v", err)
			return
		}
		builder = builder.SetType(commit).Sign(proc.signing)
		commitMsg := builder.Build()
		proc.sendMessage(commitMsg)
	}
}

func (proc *consensusProcess) beginNotifyRound() {
	// release proposal & commit trackers
	defer func() {
		proc.commitTracker = nil
		proc.proposalTracker = nil
	}()

	// send notify message only once
	if proc.notifySent {
		proc.Info("Begin notify round: notify already sent")
		return
	}

	if proc.proposalTracker.IsConflicting() {
		proc.Warning("Begin notify round: proposal is conflicting")
		return
	}

	if !proc.commitTracker.HasEnoughCommits() {
		proc.With().Warning("Begin notify round: not enough commits", log.Int("expected", proc.cfg.F+1), log.Int("actual", proc.commitTracker.CommitCount()))
		return
	}

	cert := proc.commitTracker.BuildCertificate()
	if cert == nil {
		proc.Error("Begin notify round: Build certificate returned nil")
		return
	}

	s := proc.proposalTracker.ProposedSet()
	if s == nil {
		proc.Error("Begin notify round: ProposedSet returned nil")
		return
	}

	// update set & matching certificate
	proc.s = s
	proc.certificate = cert

	// check participation
	if !proc.shouldParticipate() {
		return
	}

	// build & send notify message
	builder, err := proc.initDefaultBuilder(proc.s)
	if err != nil {
		proc.Error("init default builder failed: %v", err)
		return
	}

	builder = builder.SetType(notify).SetCertificate(proc.certificate).Sign(proc.signing)
	notifyMsg := builder.Build()
	if proc.sendMessage(notifyMsg) { // on success, mark sent
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
func (proc *consensusProcess) onRoundBegin() {
	// reset trackers
	switch proc.currentRound() {
	case statusRound:
		proc.beginStatusRound()
	case proposalRound:
		proc.beginProposalRound()
	case commitRound:
		proc.beginCommitRound()
	case notifyRound:
		proc.beginNotifyRound()
	default:
		proc.Panic("Current round out of bounds. Expected: 0-3, Found: ", proc.currentRound())
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
	proof, err := proc.oracle.Proof(types.LayerID(proc.instanceID), proc.k)
	if err != nil {
		proc.Error("Could not initialize default builder err=%v", err)
		return nil, err
	}
	builder.SetRoleProof(proof)

	return builder, nil
}

func (proc *consensusProcess) processPreRoundMsg(msg *Msg) {
	proc.preRoundTracker.OnPreRound(msg)
}

func (proc *consensusProcess) processStatusMsg(msg *Msg) {
	// record status
	proc.statusesTracker.RecordStatus(msg)
}

func (proc *consensusProcess) processProposalMsg(msg *Msg) {
	currRnd := proc.currentRound()

	if currRnd == proposalRound { // regular proposal
		proc.proposalTracker.OnProposal(msg)
	} else if currRnd == commitRound { // late proposal
		proc.proposalTracker.OnLateProposal(msg)
	} else {
		proc.Error("Received proposal message for processing in an invalid context: K=%v", msg.InnerMsg.K)
	}
}

func (proc *consensusProcess) processCommitMsg(msg *Msg) {
	proc.mTracker.Track(msg) // a commit msg passed for processing is assumed to be valid
	proc.commitTracker.OnCommit(msg)
}

func (proc *consensusProcess) processNotifyMsg(msg *Msg) {
	s := NewSet(msg.InnerMsg.Values)

	if ignored := proc.notifyTracker.OnNotify(msg); ignored {
		proc.Warning("Ignoring notification sent from %v", msg.PubKey.ShortString())
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
		proc.Debug("Not enough notifications for termination. Expected: %v Actual: %v",
			proc.cfg.F+1, proc.notifyTracker.NotificationsCount(s))
		return
	}

	// enough notifications, should terminate
	proc.s = s // update to the agreed set
	proc.Event().Info("Consensus process terminated", log.String("current_set", proc.s.String()),
		types.LayerID(proc.instanceID), log.Int("set_size", proc.s.Size()))
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
	proc.Event().Info("status round ended", log.Bool("is_svp_ready", proc.statusesTracker.IsSVPReady()),
		types.LayerID(proc.instanceID), log.String("analyze_duration", time.Since(before).String()))
}

// checks if we should participate in the current round
// returns true if we should participate, false otherwise
func (proc *consensusProcess) shouldParticipate() bool {
	// query if identity is active
	res, err := proc.oracle.IsIdentityActiveOnConsensusView(proc.signing.PublicKey().String(), types.LayerID(proc.instanceID))
	if err != nil {
		proc.With().Error("should not participate: error checking our identity for activeness",
			log.Err(err), types.LayerID(proc.instanceID))
		return false
	}

	if !res {
		proc.With().Info("should not participate: identity is not active",
			types.LayerID(proc.instanceID))
		return false
	}

	if role := proc.currentRole(); role == passive {
		proc.With().Info("should not participate: passive",
			log.Int32("round", proc.k), types.LayerID(proc.instanceID))
		return false
	}

	// should participate
	proc.With().Info("should participate",
		log.Int32("round", proc.k),
		types.LayerID(proc.instanceID))
	return true
}

// Returns the role matching the current round if eligible for this round, false otherwise
func (proc *consensusProcess) currentRole() role {
	proof, err := proc.oracle.Proof(types.LayerID(proc.instanceID), proc.k)
	if err != nil {
		proc.With().Error("Could not retrieve proof from oracle", log.Err(err))
		return passive
	}

	res, err := proc.oracle.Eligible(types.LayerID(proc.instanceID), proc.k, expectedCommitteeSize(proc.k, proc.cfg.N, proc.cfg.ExpectedLeaders), proc.nid, proof)
	if err != nil {
		proc.With().Error("Could not check our eligibility", log.Err(err))
		return passive
	}

	if res { // eligible
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
