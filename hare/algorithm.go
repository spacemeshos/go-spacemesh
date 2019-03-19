package hare

import (
	"encoding/binary"
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/metrics"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"hash/fnv"
	"time"
)

const protoName = "HARE_PROTOCOL"

type Byteable interface {
	Bytes() []byte
}

type NetworkService interface {
	RegisterGossipProtocol(protocol string) chan service.GossipMessage
	Broadcast(protocol string, payload []byte) error
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
	k           int32           // the round counter (r%4 is the round number)
	ki          int32           // indicates when S was first committed upon
	s           *Set            // the set of values
	certificate *pb.Certificate // the certificate
}

type ConsensusProcess struct {
	log.Log
	State
	Closer
	instanceId        InstanceId  // the id of this consensus instance
	oracle            HareRolacle // roles oracle
	signing           Signing
	network           NetworkService
	isStarted         bool
	inbox             chan *pb.HareMessage
	terminationReport chan TerminationOutput
	validator         messageValidator
	preRoundTracker   *PreRoundTracker
	statusesTracker   *StatusTracker
	proposalTracker   proposalTracker
	commitTracker     commitTracker
	notifyTracker     *NotifyTracker
	terminating       bool
	cfg               config.Config
	notifySent        bool
	pending           map[string]*pb.HareMessage
}

// Creates a new consensus process instance
func NewConsensusProcess(cfg config.Config, instanceId InstanceId, s *Set, oracle Rolacle, signing Signing, p2p NetworkService, terminationReport chan TerminationOutput, logger log.Log) *ConsensusProcess {
	proc := &ConsensusProcess{}
	proc.State = State{-1, -1, s.Clone(), nil}
	proc.Closer = NewCloser()
	proc.instanceId = instanceId
	proc.oracle = NewHareOracle(oracle, cfg.N)
	proc.signing = signing
	proc.network = p2p
	proc.validator = newSyntaxContextValidator(signing, cfg.F+1, proc.statusValidator(), logger)
	proc.preRoundTracker = NewPreRoundTracker(cfg.F+1, cfg.N)
	proc.notifyTracker = NewNotifyTracker(cfg.N)
	proc.terminating = false
	proc.cfg = cfg
	proc.notifySent = false
	proc.terminationReport = terminationReport
	proc.pending = make(map[string]*pb.HareMessage, cfg.N)
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
func (proc *ConsensusProcess) SetInbox(inbox chan *pb.HareMessage) {
	if inbox == nil {
		proc.Error("ConsensusProcess tried to SetInbox with nil")
		return
	}

	proc.inbox = inbox
}

func (proc *ConsensusProcess) eventLoop() {
	proc.With().Info("Consensus Processes Started",
		log.Int("N", proc.cfg.N), log.Int("f", proc.cfg.F), log.String("duration", (time.Duration(proc.cfg.RoundDuration)*time.Second).String()),
		log.Uint32("instance_id", uint32(proc.instanceId)), log.String("set_values", proc.s.String()))

	// set pre-round message and send
	m := proc.initDefaultBuilder(proc.s).SetType(PreRound).Sign(proc.signing).Build()
	proc.sendMessage(m)

	// listen to pre-round messages
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
		proc.Error("Fatal: PreRound ended with empty set")
	}
	proc.advanceToNextRound() // k was initialized to -1, k should be 0

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
			proc.Info("Stop event loop, terminating")
			return
		}
	}
}

func (proc *ConsensusProcess) onEarlyMessage(m *pb.HareMessage) {
	if m == nil {
		proc.Error("onEarlyMessage called with nil")
		return
	}

	verifier, err := NewVerifier(m.PubKey)
	if err != nil {
		proc.Warning("Could not construct verifier: ", err)
		return
	}

	if _, exist := proc.pending[verifier.String()]; exist { // ignore, already received
		proc.Warning("Already received message from sender %v", verifier.String())
		return
	}

	proc.pending[verifier.String()] = m
}

func (proc *ConsensusProcess) handleMessage(m *pb.HareMessage) {
	// Note: instanceId is already verified by the broker

	proc.Debug("Received message %v", m)

	if !proc.validator.SyntacticallyValidateMessage(m) {
		proc.Warning("Syntactically validation failed, pubkey %v", m.PubKey)
		return
	}

	mType := MessageType(m.Message.Type).String()
	// validate message for this or next round
	if !proc.validator.ContextuallyValidateMessage(m, proc.k) {
		if !proc.validator.ContextuallyValidateMessage(m, proc.k+1) {
			// TODO: should return error from message validation to indicate what failed, should retry only for contextual failure
			proc.Warning("Message of type %v is not valid for either round, pubkey %v", mType, m.PubKey)
			return
		} else { // a valid early message, keep it for later
			proc.Info("Early message of type %v detected. Keeping message, pubkey %v", mType, m.PubKey)
			proc.onEarlyMessage(m)
			return
		}
	}

	// continue process msg by type
	proc.processMsg(m)
}

func (proc *ConsensusProcess) processMsg(m *pb.HareMessage) {
	proc.Info("Processing message of type %v", MessageType(m.Message.Type).String())

	metrics.MessageTypeCounter.With("type_id", MessageType(m.Message.Type).String()).Add(1)

	switch MessageType(m.Message.Type) {
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
		proc.Warning("Unknown message type: %v , pubkey %v", m.Message.Type, m.PubKey)
	}
}

func (proc *ConsensusProcess) sendMessage(msg *pb.HareMessage) {
	// invalid msg
	if msg == nil {
		proc.Error("sendMessage was called with nil")
		return
	}

	// check eligibility
	if !proc.isEligible() {
		proc.Info("%v not eligible on round %v", proc.signing.Verifier(), proc.k)
		return
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		proc.Error("failed marshaling message")
		panic("could not marshal message before send")
	}

	if err := proc.network.Broadcast(protoName, data); err != nil {
		proc.Error("Could not broadcast round message ", err.Error())
		return
	}

	proc.Info("Message of type %v sent", MessageType(msg.Message.Type).String())
}

func (proc *ConsensusProcess) onRoundEnd() {
	proc.With().Info("End of round", log.Int32("k", proc.k))

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
			log.Bool("is_conflicting", proc.proposalTracker.IsConflicting()))
	case Round3:
		proc.endOfRound3()
	}
}

func (proc *ConsensusProcess) advanceToNextRound() {
	proc.k++
}

func (proc *ConsensusProcess) beginRound1() {
	proc.statusesTracker = NewStatusTracker(proc.cfg.F+1, proc.cfg.N)
	proc.statusesTracker.Log = proc.Log
	statusMsg := proc.initDefaultBuilder(proc.s).SetType(Status).Sign(proc.signing).Build()
	proc.sendMessage(statusMsg)
}

func (proc *ConsensusProcess) beginRound2() {
	proc.proposalTracker = NewProposalTracker(proc.Log)

	if proc.isEligible() && proc.statusesTracker.IsSVPReady() {
		builder := proc.initDefaultBuilder(proc.statusesTracker.ProposalSet(defaultSetSize))
		svp := proc.statusesTracker.BuildSVP()
		if svp != nil {
			proposalMsg := builder.SetType(Proposal).SetSVP(svp).Sign(proc.signing).Build()
			proc.sendMessage(proposalMsg)
		} else {
			proc.Error("Failed to build SVP (nil) after verifying SVP is ready ")
		}
	}

	// done with building proposal, reset statuses tracking
	proc.statusesTracker = nil
}

func (proc *ConsensusProcess) beginRound3() {
	proposedSet := proc.proposalTracker.ProposedSet()

	// proposedSet may be nil, in such case the tracker will ignore messages
	proc.commitTracker = NewCommitTracker(proc.cfg.F+1, proc.cfg.N, proposedSet) // track commits for proposed set

	if proposedSet != nil { // has proposal to send
		builder := proc.initDefaultBuilder(proposedSet).SetType(Commit).Sign(proc.signing)
		commitMsg := builder.Build()
		proc.sendMessage(commitMsg)
	}
}

func (proc *ConsensusProcess) beginRound4() {
	proc.commitTracker = nil
	proc.proposalTracker = nil
}

func (proc *ConsensusProcess) handlePending(pending map[string]*pb.HareMessage) {
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
		proc.Error("Current round out of bounds. Expected: 0-4, Found: ", proc.currentRound())
		panic("Current round out of bounds")
	}

	pendingProcess := proc.pending
	proc.pending = make(map[string]*pb.HareMessage, proc.cfg.N)
	go proc.handlePending(pendingProcess)
}

func (proc *ConsensusProcess) roleProof() Signature {
	kInBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(kInBytes, uint32(proc.k))
	hash := fnv.New32()
	hash.Write(proc.signing.Verifier().Bytes())
	hash.Write(kInBytes)

	hashBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(hashBytes, uint32(hash.Sum32()))

	return proc.signing.Sign(hashBytes)
}

func (proc *ConsensusProcess) initDefaultBuilder(s *Set) *MessageBuilder {
	builder := NewMessageBuilder().SetPubKey(proc.signing.Verifier().Bytes()).SetInstanceId(proc.instanceId)
	builder = builder.SetRoundCounter(proc.k).SetKi(proc.ki).SetValues(s)
	builder.SetRoleProof(proc.roleProof())

	return builder
}

func (proc *ConsensusProcess) processPreRoundMsg(msg *pb.HareMessage) {
	proc.preRoundTracker.OnPreRound(msg)
}

func (proc *ConsensusProcess) processStatusMsg(msg *pb.HareMessage) {
	// record status
	proc.statusesTracker.RecordStatus(msg)
}

func (proc *ConsensusProcess) processProposalMsg(msg *pb.HareMessage) {
	if proc.currentRound() == Round2 { // regular proposal
		proc.proposalTracker.OnProposal(msg)
	} else { // late proposal
		proc.proposalTracker.OnLateProposal(msg)
	}
}

func (proc *ConsensusProcess) processCommitMsg(msg *pb.HareMessage) {
	proc.commitTracker.OnCommit(msg)
}

func (proc *ConsensusProcess) processNotifyMsg(msg *pb.HareMessage) {
	s := NewSet(msg.Message.Values)

	if ignored := proc.notifyTracker.OnNotify(msg); ignored {
		proc.Warning("Ignoring notification sent from %v", msg.PubKey)
		return
	}

	if proc.currentRound() == Round4 { // not necessary to update otherwise
		// we assume that this expression was checked before
		if int32(msg.Cert.AggMsgs.Messages[0].Message.K) >= proc.ki { // update state iff K >= ki
			proc.s = s
			proc.certificate = msg.Cert
			proc.ki = msg.Message.Ki
		}
	}

	if proc.notifyTracker.NotificationsCount(s) < proc.cfg.F+1 { // not enough
		proc.Debug("Not enough notifications for termination. Expected: %v Actual: %v",
			proc.cfg.F+1, proc.notifyTracker.NotificationsCount(s))
		return
	}

	// enough notifications, should terminate
	proc.s = s // update to the agreed set
	proc.With().Info("Consensus process terminated", log.String("set_values", proc.s.String()))
	proc.terminationReport <- procOutput{proc.instanceId, proc.s}
	proc.Close()
	proc.terminating = true // ensures immediate termination
}

func (proc *ConsensusProcess) currentRound() int {
	return int(proc.k % 4)
}

func (proc *ConsensusProcess) statusValidator() func(m *pb.HareMessage) bool {
	validate := func(m *pb.HareMessage) bool {
		s := NewSet(m.Message.Values)
		if m.Message.Ki == -1 { // no certificates, validate by pre-round msgs
			if proc.preRoundTracker.CanProveSet(s) { // can prove s
				return true
			}
		} else { // ki>=0, we should have received a certificate for that set
			if proc.notifyTracker.HasCertificate(m.Message.Ki, s) { // can prove s
				return true
			}
		}
		return false
	}

	return validate
}

func (proc *ConsensusProcess) endOfRound1() {
	proc.statusesTracker.AnalyzeStatuses(proc.statusValidator())
	proc.With().Info("Round 1 ended", log.Bool("is_svp_ready", proc.statusesTracker.IsSVPReady()))
}

func (proc *ConsensusProcess) endOfRound3() {
	// notify already sent after committing, only one should be sent
	if proc.notifySent {
		proc.Info("End of round 3: notification already sent")
		return
	}

	if proc.proposalTracker.IsConflicting() {
		proc.Warning("End of round 3: proposal is conflicting")
		return
	}

	if !proc.commitTracker.HasEnoughCommits() {
		proc.Warning("End of round 3: not enough commits")
		return
	}

	cert := proc.commitTracker.BuildCertificate()
	if cert == nil {
		proc.Error("Build certificate returned nil")
		return
	}

	s := proc.proposalTracker.ProposedSet()
	if s == nil {
		proc.Error("ProposedSet returned nil")
		return
	}

	// commit & send notification message
	proc.Debug("end of round 3: committing on %v and sending notification message", s)
	proc.s = s
	proc.certificate = cert
	builder := proc.initDefaultBuilder(proc.s).SetType(Notify).SetCertificate(proc.certificate).Sign(proc.signing)
	notifyMsg := builder.Build()
	proc.sendMessage(notifyMsg)
	proc.notifySent = true
}

func (proc *ConsensusProcess) isEligible() bool {
	return proc.currentRole() != Passive
}

// Returns the role matching the current round if eligible for this round, false otherwise
func (proc *ConsensusProcess) currentRole() Role {
	if proc.oracle.Eligible(proc.instanceId, proc.k, proc.signing.Verifier().String(), proc.roleProof()) {
		if proc.currentRound() == Round2 {
			return Leader
		}
		return Active
	}

	return Passive
}
