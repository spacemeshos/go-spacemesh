package hare

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/metrics"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/types"
	"time"
)

const protoName = "HARE_PROTOCOL"

type Rolacle interface {
	Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeId, sig []byte) (bool, error)
	Proof(id types.NodeId, layer types.LayerID, round int32) ([]byte, error)
}

type NetworkService interface {
	RegisterGossipProtocol(protocol string) chan service.GossipMessage
	Broadcast(protocol string, payload []byte) error
}

type Signer interface {
	Sign(m []byte) []byte
	PublicKey() *signing.PublicKey
}

type procOutput struct {
	id  InstanceId
	set *Set
}

func (cpo procOutput) Id() InstanceId {
	return cpo.id
}

func (cpo procOutput) Set() *Set {
	return cpo.set
}

var _ TerminationOutput = (*procOutput)(nil)

// Represents the state of the participant
type State struct {
	k           int32        // the round counter (r%4 is the round number)
	ki          int32        // indicates when S was first committed upon
	s           *Set         // the set of Values
	certificate *Certificate // the certificate
}

type StateQuerier interface {
	IsIdentityActive(edId string, layer types.LayerID) (bool, types.AtxId, error)
}

type Msg struct {
	*Message
	PubKey *signing.PublicKey
}

func (m *Msg) String() string {
	return fmt.Sprintf("Pubkey: %v Message: %v", m.PubKey.ShortString(), m.Message.String())
}

func (m *Msg) Bytes() []byte {
	var w bytes.Buffer
	_, err := xdr.Marshal(&w, m.Message)
	if err != nil {
		log.Panic("could not marshal InnerMsg before send")
	}

	return w.Bytes()
}

func newMsg(hareMsg *Message, querier StateQuerier, layersPerEpoch uint16) (*Msg, error) {
	// extract pub key
	pubKey, err := ed25519.ExtractPublicKey(hareMsg.InnerMsg.Bytes(), hareMsg.Sig)
	if err != nil {
		log.Error("newMsg construction failed: could not extract public key err=%v", err, len(hareMsg.Sig))
		return nil, err
	}
	// query if identity is active
	pub := signing.NewPublicKey(pubKey)
	res, _, err := querier.IsIdentityActive(pub.String(), types.LayerID(hareMsg.InnerMsg.InstanceId))
	if err != nil {
		log.Error("error while checking if identity is active for %v err=%v", pub.String(), err)
		return nil, errors.New("is identity active query failed")
	}

	// check query result
	if !res {
		log.Error("identity %v is not active", pub.ShortString())
		return nil, errors.New("inactive identity")
	}

	return &Msg{hareMsg, pub}, nil
}

type ConsensusProcess struct {
	log.Log
	State
	Closer
	instanceId        InstanceId // the id of this consensus instance
	oracle            Rolacle    // roles oracle
	signing           Signer
	nid               types.NodeId
	network           NetworkService
	isStarted         bool
	inbox             chan *Msg
	terminationReport chan TerminationOutput
	stateQuerier      StateQuerier
	validator         messageValidator
	preRoundTracker   *PreRoundTracker
	statusesTracker   *StatusTracker
	proposalTracker   proposalTracker
	commitTracker     commitTracker
	notifyTracker     *NotifyTracker
	terminating       bool
	cfg               config.Config
	notifySent        bool
	pending           map[string]*Msg
}

// Creates a new consensus process instance
func NewConsensusProcess(cfg config.Config, instanceId InstanceId, s *Set, oracle Rolacle, stateQuerier StateQuerier, layersPerEpoch uint16, signing Signer, nid types.NodeId, p2p NetworkService, terminationReport chan TerminationOutput, logger log.Log) *ConsensusProcess {
	proc := &ConsensusProcess{}
	proc.State = State{-1, -1, s.Clone(), nil}
	proc.Closer = NewCloser()
	proc.instanceId = instanceId
	proc.oracle = oracle
	proc.signing = signing
	proc.nid = nid
	proc.network = p2p
	proc.stateQuerier = stateQuerier
	proc.validator = newSyntaxContextValidator(signing, cfg.F+1, proc.statusValidator(), stateQuerier, layersPerEpoch, logger)
	proc.preRoundTracker = NewPreRoundTracker(cfg.F+1, cfg.N)
	proc.notifyTracker = NewNotifyTracker(cfg.N)
	proc.terminating = false
	proc.cfg = cfg
	proc.notifySent = false
	proc.terminationReport = terminationReport
	proc.pending = make(map[string]*Msg, cfg.N)
	proc.Log = logger

	return proc
}

// Returns the iteration number from a given round counter
func iterationFromCounter(roundCounter int32) int32 {
	return roundCounter / 4
}

// Starts the consensus process
func (proc *ConsensusProcess) Start() error {
	if proc.isStarted { // called twice on same instance
		proc.Error("ConsensusProcess has already been started")
		return StartInstanceError(errors.New("instance already started"))
	}

	if proc.s.Size() == 0 { // empty set is not valid
		proc.Error("ConsensusProcess cannot be started with an empty set")
		return StartInstanceError(errors.New("instance started with an empty set"))
	}

	if proc.inbox == nil { // no inbox
		proc.Error("ConsensusProcess cannot be started with nil inbox")
		return StartInstanceError(errors.New("instance started with nil inbox"))
	}

	proc.isStarted = true

	go proc.eventLoop()

	return nil
}

// Returns the id of this instance
func (proc *ConsensusProcess) Id() InstanceId {
	return proc.instanceId
}

// Sets the inbox channel
func (proc *ConsensusProcess) SetInbox(inbox chan *Msg) {
	if inbox == nil {
		proc.Error("ConsensusProcess tried to SetInbox with nil")
		return
	}

	proc.inbox = inbox
}

func (proc *ConsensusProcess) eventLoop() {
	proc.With().Info("Consensus Process Started",
		log.Int("Hare-N", proc.cfg.N), log.Int("f", proc.cfg.F), log.String("duration", (time.Duration(proc.cfg.RoundDuration)*time.Second).String()),
		log.Uint64("layer_id", uint64(proc.instanceId)), log.Int("exp_leaders", proc.cfg.ExpectedLeaders), log.String("set_values", proc.s.String()))

	// set pre-round InnerMsg and send
	builder, err := proc.initDefaultBuilder(proc.s)
	if err != nil {
		proc.Error("init default builder failed: %v", err)
		return
	}
	m := builder.SetType(PreRound).Sign(proc.signing).Build()
	proc.sendMessage(m)

	// listen to pre-round Messages
	timer := time.NewTimer(time.Duration(proc.cfg.RoundDuration) * time.Second)
PreRound:
	for {
		select {
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
		proc.With().Error("Fatal: PreRound ended with empty set", log.Uint64("layer_id", uint64(proc.instanceId)))
	}
	proc.advanceToNextRound() // K was initialized to -1, K should be 0

	// start first iteration
	proc.onRoundBegin()
	ticker := time.NewTicker(time.Duration(proc.cfg.RoundDuration) * time.Second)
	for {
		select {
		case msg := <-proc.inbox: // msg event
			proc.handleMessage(msg)
			if proc.terminating {
				proc.Info("Detected terminating on. Exiting.")
				return
			}
		case <-ticker.C: // next round event
			proc.onRoundEnd()
			proc.advanceToNextRound()
			proc.onRoundBegin()
		case <-proc.CloseChannel(): // close event
			proc.Debug("Stop event loop, terminating")
			return
		}
	}
}

func (proc *ConsensusProcess) onEarlyMessage(m *Msg) {
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

func (proc *ConsensusProcess) handleMessage(m *Msg) {
	// Note: InstanceId is already verified by the broker

	proc.Info("Received message %v (msg_id %v)", m, hex.EncodeToString(m.Sig[:5]))

	if !proc.validator.SyntacticallyValidateMessage(m) {
		proc.Warning("Syntactically validation failed, pubkey %v", m.PubKey.ShortString())
		return
	}

	mType := MessageType(m.InnerMsg.Type).String()
	// validate InnerMsg for this or next round
	if !proc.validator.ContextuallyValidateMessage(m, proc.k) {
		if !proc.validator.ContextuallyValidateMessage(m, proc.k+1) {
			// TODO: should return error from InnerMsg validation to indicate what failed, should retry only for contextual failure
			proc.Warning("message of type %v is not valid for either round, pubkey %v. Expected: %v, Actual: %v",
				mType, m.PubKey.ShortString(), proc.k+1, m.InnerMsg.K)
			return
		} else { // a valid early InnerMsg, keep it for later
			proc.Debug("Early message of type %v detected. Keeping message, pubkey %v", mType, m.PubKey.ShortString())
			proc.onEarlyMessage(m)
			return
		}
	}

	// continue process msg by type
	proc.processMsg(m)
}

func (proc *ConsensusProcess) processMsg(m *Msg) {
	proc.Info("Processing message of type %v (msg_id %v)", m.InnerMsg.Type.String(), hex.EncodeToString(m.Sig[:5]))
	metrics.MessageTypeCounter.With("type_id", m.InnerMsg.Type.String()).Add(1)

	switch m.InnerMsg.Type {
	case PreRound:
		proc.processPreRoundMsg(m)
	case Status: // end of round 1
		proc.processStatusMsg(m)
	case Proposal: // end of round 2
		proc.processProposalMsg(m)
	case Commit: // end of round 3
		proc.processCommitMsg(m)
	case Notify: // end of round 4
		proc.processNotifyMsg(m)
	default:
		proc.Warning("Unknown message type: %v , pubkey %v", m.InnerMsg.Type, m.PubKey.ShortString())
	}
}

func (proc *ConsensusProcess) sendMessage(msg *Msg) {
	// invalid msg
	if msg == nil {
		proc.Error("sendMessage was called with nil")
		return
	}

	// check participation
	if !proc.shouldParticipate() {
		proc.With().Info("Should not participate", log.Int32("round", proc.k),
			log.Uint64("layer_id", uint64(proc.instanceId)))
		return
	}

	if err := proc.network.Broadcast(protoName, msg.Bytes()); err != nil {
		proc.Error("Could not broadcast round message ", err.Error())
		return
	}

	proc.With().Info("message sent", log.String("msg_type", msg.InnerMsg.Type.String()),
		log.Uint64("layer_id", uint64(proc.instanceId)), log.String("msg_id", hex.EncodeToString(msg.Sig[:5])))
}

func (proc *ConsensusProcess) onRoundEnd() {
	proc.With().Debug("End of round", log.Int32("K", proc.k), log.Uint64("layer_id", uint64(proc.instanceId)))

	// reset trackers
	switch proc.currentRound() {
	case Round1:
		proc.endOfRound1()
	case Round2:
		s := proc.proposalTracker.ProposedSet()
		sStr := "nil"
		if s != nil {
			sStr = s.String()
		}
		proc.With().Info("Round 2 ended",
			log.String("proposed_set", sStr),
			log.Bool("is_conflicting", proc.proposalTracker.IsConflicting()),
			log.Uint64("layer_id", uint64(proc.instanceId)))
	case Round3:
		proc.endOfRound3()
	}
}

func (proc *ConsensusProcess) advanceToNextRound() {
	proc.k++
	if proc.k >= 4 && proc.k%4 == 0 {
		proc.With().Warning("Starting new iteration", log.Int32("round_counter", proc.k),
			log.Uint64("layer_id", uint64(proc.instanceId)))
	}
}

func (proc *ConsensusProcess) beginRound1() {
	proc.statusesTracker = NewStatusTracker(proc.cfg.F+1, proc.cfg.N)
	proc.statusesTracker.Log = proc.Log
	b, err := proc.initDefaultBuilder(proc.s)
	if err != nil {
		proc.Error("init default builder failed: %v", err)
		return
	}
	statusMsg := b.SetType(Status).Sign(proc.signing).Build()
	proc.sendMessage(statusMsg)
}

func (proc *ConsensusProcess) beginRound2() {
	proc.proposalTracker = NewProposalTracker(proc.Log)

	// done with building proposal, reset statuses tracking
	defer func() { proc.statusesTracker = nil }()

	if proc.shouldParticipate() && proc.statusesTracker.IsSVPReady() {
		builder, err := proc.initDefaultBuilder(proc.statusesTracker.ProposalSet(defaultSetSize))
		if err != nil {
			proc.Error("init default builder failed: %v", err)
			return
		}
		svp := proc.statusesTracker.BuildSVP()
		if svp != nil {
			proposalMsg := builder.SetType(Proposal).SetSVP(svp).Sign(proc.signing).Build()
			proc.sendMessage(proposalMsg)
		} else {
			proc.Error("Failed to build SVP (nil) after verifying SVP is ready ")
		}
	}
}

func (proc *ConsensusProcess) beginRound3() {
	proposedSet := proc.proposalTracker.ProposedSet()

	// proposedSet may be nil, in such case the tracker will ignore Messages
	proc.commitTracker = NewCommitTracker(proc.cfg.F+1, proc.cfg.N, proposedSet) // track commits for proposed set

	if proposedSet != nil { // has proposal to commit on
		builder, err := proc.initDefaultBuilder(proposedSet)
		if err != nil {
			proc.Error("init default builder failed: %v", err)
			return
		}
		builder = builder.SetType(Commit).Sign(proc.signing)
		commitMsg := builder.Build()
		proc.sendMessage(commitMsg)
	}
}

func (proc *ConsensusProcess) beginRound4() {
	proc.commitTracker = nil
	proc.proposalTracker = nil
}

func (proc *ConsensusProcess) handlePending(pending map[string]*Msg) {
	for _, m := range pending {
		proc.inbox <- m
	}
}

func (proc *ConsensusProcess) onRoundBegin() {
	// reset trackers
	switch proc.currentRound() {
	case Round1:
		proc.beginRound1()
	case Round2:
		proc.beginRound2()
	case Round3:
		proc.beginRound3()
	case Round4:
		proc.beginRound4()
	default:
		proc.Panic("Current round out of bounds. Expected: 0-3, Found: ", proc.currentRound())
	}

	pendingProcess := proc.pending
	proc.pending = make(map[string]*Msg, proc.cfg.N)
	go proc.handlePending(pendingProcess)
}

func (proc *ConsensusProcess) initDefaultBuilder(s *Set) (*MessageBuilder, error) {
	builder := NewMessageBuilder().SetInstanceId(proc.instanceId)
	builder = builder.SetRoundCounter(proc.k).SetKi(proc.ki).SetValues(s)
	proof, err := proc.oracle.Proof(proc.nid, types.LayerID(proc.instanceId), proc.k)
	if err != nil {
		proc.Error("Could not initialize default builder err=%v", err)
		return nil, err
	}
	builder.SetRoleProof(proof)

	return builder, nil
}

func (proc *ConsensusProcess) processPreRoundMsg(msg *Msg) {
	proc.preRoundTracker.OnPreRound(msg)
}

func (proc *ConsensusProcess) processStatusMsg(msg *Msg) {
	// record status
	proc.statusesTracker.RecordStatus(msg)
}

func (proc *ConsensusProcess) processProposalMsg(msg *Msg) {
	currRnd := proc.currentRound()

	if currRnd == Round2 { // regular proposal
		proc.proposalTracker.OnProposal(msg)
	} else if currRnd == Round3 { // late proposal
		proc.proposalTracker.OnLateProposal(msg)
	} else {
		proc.Error("Received proposal message for processing in an invalid context: K=%v", msg.InnerMsg.K)
	}
}

func (proc *ConsensusProcess) processCommitMsg(msg *Msg) {
	proc.commitTracker.OnCommit(msg)
}

func (proc *ConsensusProcess) processNotifyMsg(msg *Msg) {
	s := NewSet(msg.InnerMsg.Values)

	if ignored := proc.notifyTracker.OnNotify(msg); ignored {
		proc.Warning("Ignoring notification sent from %v", msg.PubKey.ShortString())
		return
	}

	if proc.currentRound() == Round4 { // not necessary to update otherwise
		// we assume that this expression was checked before
		if int32(msg.InnerMsg.Cert.AggMsgs.Messages[0].InnerMsg.K) >= proc.ki { // update state iff K >= Ki
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
	proc.With().Info("Consensus process terminated", log.String("set_values", proc.s.String()),
		log.Uint64("layer_id", uint64(proc.instanceId)))
	proc.terminationReport <- procOutput{proc.instanceId, proc.s}
	proc.Close()
	proc.terminating = true // ensures immediate termination
}

func (proc *ConsensusProcess) currentRound() int {
	return int(proc.k % 4)
}

func (proc *ConsensusProcess) statusValidator() func(m *Msg) bool {
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

func (proc *ConsensusProcess) endOfRound1() {
	proc.statusesTracker.AnalyzeStatuses(proc.statusValidator())
	proc.With().Info("Round 1 ended", log.Bool("is_svp_ready", proc.statusesTracker.IsSVPReady()),
		log.Uint64("layer_id", uint64(proc.instanceId)))
}

func (proc *ConsensusProcess) endOfRound3() {
	// notify already sent after committing, only one should be sent
	if proc.notifySent {
		proc.Info("Round 3 ended: notification already sent")
		return
	}

	if proc.proposalTracker.IsConflicting() {
		proc.Warning("Round 3 ended: proposal is conflicting")
		return
	}

	if !proc.commitTracker.HasEnoughCommits() {
		proc.Warning("Round 3 ended: not enough commits")
		return
	}

	cert := proc.commitTracker.BuildCertificate()
	if cert == nil {
		proc.Error("Round 3 ended: Build certificate returned nil")
		return
	}

	s := proc.proposalTracker.ProposedSet()
	if s == nil {
		proc.Error("Round 3 ended: ProposedSet returned nil")
		return
	}

	// commit & send notification msg
	proc.With().Info("Round 3 ended: committing", log.String("committed_set", s.String()),
		log.Uint64("layer_id", uint64(proc.instanceId)))
	proc.s = s
	proc.certificate = cert
	builder, err := proc.initDefaultBuilder(proc.s)
	if err != nil {
		proc.Error("init default builder failed: %v", err)
		return
	}
	builder = builder.SetType(Notify).SetCertificate(proc.certificate).Sign(proc.signing)
	notifyMsg := builder.Build()
	proc.sendMessage(notifyMsg)
	proc.notifySent = true
}

func (proc *ConsensusProcess) shouldParticipate() bool {
	// query if identity is active
	res, _, err := proc.stateQuerier.IsIdentityActive(proc.signing.PublicKey().String(), types.LayerID(proc.instanceId))
	if err != nil {
		proc.With().Error("Error checking our identity for activeness", log.String("err", err.Error()),
			log.Uint64("layer_id", uint64(proc.instanceId)))
		return false
	}

	if !res {
		proc.With().Info("Should not participate in the protocol. Reason: identity not active",
			log.Uint64("layer_id", uint64(proc.instanceId)))
		return false
	}

	return proc.currentRole() != Passive
}

// Returns the role matching the current round if eligible for this round, false otherwise
func (proc *ConsensusProcess) currentRole() Role {
	proof, err := proc.oracle.Proof(proc.nid, types.LayerID(proc.instanceId), proc.k)
	if err != nil {
		proc.Error("Could not retrieve proof from oracle err=%v", err)
		return Passive
	}

	res, err := proc.oracle.Eligible(types.LayerID(proc.instanceId), proc.k, expectedCommitteeSize(proc.k, proc.cfg.N, proc.cfg.ExpectedLeaders), proc.nid, proof)
	if err != nil {
		proc.Error("Error checking our eligibility: %v", err)
		return Passive
	}

	if res { // eligible
		if proc.currentRound() == Round2 {
			return Leader
		}
		return Active
	}

	return Passive
}

// Returns the expected committee size for the given round assuming maxExpActives is the default size
func expectedCommitteeSize(k int32, maxExpActive, expLeaders int) int {
	if k%4 == Round2 {
		return expLeaders // expected number of leaders
	}

	// N actives in any other case
	return maxExpActive
}
