package hare

import (
	"encoding/binary"
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"time"
)

const ProtoName = "HARE_PROTOCOL"

type Byteable interface {
	Bytes() []byte
}

type NetworkService interface {
	RegisterProtocol(protocol string) chan service.Message
	Broadcast(protocol string, payload []byte) error
}

type State struct {
	k           uint32          // the round counter (k%4 is the round number)
	ki          int32           // indicates when S was first committed upon
	s           *Set            // the set of values
	certificate *pb.Certificate // the certificate
}

type ConsensusProcess struct {
	State
	Closer // the consensus is closeable
	pubKey          crypto.PublicKey
	instanceId      InstanceId
	oracle          Rolacle // roles oracle
	signing         Signing
	network         NetworkService
	startTime       time.Time // TODO: needed?
	inbox           chan *pb.HareMessage
	role            Role // the current role
	validator       *MessageValidator
	preRoundTracker *PreRoundTracker
	statusesTracker *StatusTracker
	proposalTracker *ProposalTracker
	commitTracker   *CommitTracker
	notifyTracker   *NotifyTracker
	terminating     bool
	cfg             config.Config
}

func NewConsensusProcess(cfg config.Config, key crypto.PublicKey, instanceId InstanceId, s Set, oracle Rolacle, signing Signing, p2p NetworkService) *ConsensusProcess {
	proc := &ConsensusProcess{}
	proc.State = State{0, -1, &s, nil}
	proc.Closer = NewCloser()
	proc.pubKey = key
	proc.instanceId = instanceId
	proc.oracle = oracle
	proc.signing = signing
	proc.network = p2p
	proc.role = Passive
	proc.validator = NewMessageValidator(signing, cfg.F+1, cfg.N)
	proc.preRoundTracker = NewPreRoundTracker(cfg.F+1, cfg.N)
	proc.statusesTracker = NewStatusTracker(cfg.F+1, cfg.N)
	proc.proposalTracker = NewProposalTracker(cfg.N)
	proc.commitTracker = NewCommitTracker(cfg.F+1, cfg.N, nil)
	proc.notifyTracker = NewNotifyTracker(cfg.N)
	proc.terminating = false
	proc.cfg = cfg

	return proc
}

func (proc *ConsensusProcess) Id() uint32 {
	return proc.instanceId.Id()
}

// Returns the iteration number from a given round counter
func iterationFromCounter(roundCounter uint32) uint32 {
	return roundCounter / 4
}

func (proc *ConsensusProcess) Start() error {
	if !proc.startTime.IsZero() { // called twice on same instance
		log.Error("ConsensusProcess has already been started.")
		return StartInstanceError(errors.New("instance already started"))
	}

	proc.startTime = time.Now()

	go proc.eventLoop()

	return nil
}

func (proc *ConsensusProcess) createInbox(size uint32) chan *pb.HareMessage {
	proc.inbox = make(chan *pb.HareMessage, size)
	return proc.inbox
}

func (proc *ConsensusProcess) eventLoop() {
	log.Info("Start listening")

	// update role
	proc.updateRole()

	// set pre-round message and send
	proc.sendMessage(proc.initDefaultBuilder(proc.s).SetType(PreRound).Sign(proc.signing).Build())

	// listen to pre-round messages
	timer := time.NewTimer(proc.cfg.RoundDuration)
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
	proc.preRoundTracker.UpdateSet(proc.s)

	// start first iteration
	proc.onRoundBegin()
	ticker := time.NewTicker(proc.cfg.RoundDuration)
	for {
		select {
		case msg := <-proc.inbox: // msg event
			proc.handleMessage(msg)
		case <-ticker.C: // next round event
			proc.onRoundEnd()
			proc.advanceToNextRound()
			proc.onRoundBegin()
		case <-proc.CloseChannel(): // close event
			log.Info("Stop event loop, terminating")
			return
		}

		if proc.terminating {
			return
		}
	}
}

func roleFromRoundCounter(k uint32) Role {
	switch k % 4 {
	case Round2:
		return Leader
	case Round4:
		return Passive
	default:
		return Active
	}
}

func (proc *ConsensusProcess) handleMessage(m *pb.HareMessage) {
	// Note: instanceId is already verified by the broker

	// validate message
	if !proc.validator.ValidateMessage(m, proc.k) {
		log.Info("Message is not syntactically valid")
		return
	}

	// validate role
	if proc.oracle.Role(Signature(m.Message.RoleProof)) == roleFromRoundCounter(m.Message.K) {
		log.Warning("invalid role detected for: ", m.String())
		return
	}

	// continue process msg by type
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
		log.Warning("Unknown message type: ", m.Message.Type)
	}
}

func (proc *ConsensusProcess) sendMessage(msg *pb.HareMessage) {
	// invalid
	if msg == nil {
		return
	}

	// send only if our role matches the required role for this round
	if proc.role != roleFromRoundCounter(proc.k) {
		return
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		log.Error("failed marshaling message")
		panic("could not marshal message before send")
	}

	if err := proc.network.Broadcast(ProtoName, data); err != nil {
		log.Error("Could not broadcast round message ", err.Error())
		return
	}

	// msg successfully sent to network, send it to yourself but don't block
	go func() { proc.inbox <- msg }()
}

func (proc *ConsensusProcess) onRoundEnd() {
	log.Info("End of round: %d", proc.k)

	// reset trackers
	switch proc.currentRound() {
	case Round3:
		proc.endOfRound3()
	}
}

func (proc *ConsensusProcess) advanceToNextRound() {
	proc.k++
}

func (proc *ConsensusProcess) beginRound1() {
	roundMsg := proc.initDefaultBuilder(proc.s).SetType(Status).Sign(proc.signing).Build()
	proc.sendMessage(roundMsg)
	proc.statusesTracker = NewStatusTracker(proc.cfg.F+1, proc.cfg.N)
}

func (proc *ConsensusProcess) beginRound2() {
	if proc.role == Leader && proc.statusesTracker.IsSVPReady() {
		builder := proc.initDefaultBuilder(proc.statusesTracker.ProposalSet(proc.cfg.SetSize))
		svp := proc.statusesTracker.BuildSVP()
		if svp != nil {
			roundMsg := builder.SetType(Proposal).SetSVP(svp).Sign(proc.signing).Build()
			proc.sendMessage(roundMsg)
		} else {
			log.Error("Failed to build SVP (nil) after verifying SVP is ready ")
		}
	}

	// done with building proposal, reset statuses tracking
	proc.statusesTracker = nil
}

func (proc *ConsensusProcess) beginRound3() {
	proposedSet := proc.proposalTracker.ProposedSet()
	if proposedSet != nil { // has proposal to send
		builder := proc.initDefaultBuilder(proposedSet).SetType(Commit).Sign(proc.signing)
		roundMsg := builder.Build()
		proc.sendMessage(roundMsg)
	}

	// proposedSet may be nil, in such case the tracker will ignore messages
	proc.commitTracker = NewCommitTracker(proc.cfg.F+1, proc.cfg.N, proposedSet) // track commits for proposed set
}

func (proc *ConsensusProcess) beginRound4() {
	proc.commitTracker = nil
	proc.proposalTracker = nil
}

func (proc *ConsensusProcess) onRoundBegin() {
	proc.updateRole()

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
		log.Error("Current round out of bounds. Expected: 0-4, Found: ", proc.currentRound())
		panic("Current round out of bounds")
	}
}

func (proc *ConsensusProcess) roleProof() Signature {
	kInBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(kInBytes, uint32(proc.k))

	return proc.signing.Sign(kInBytes)
}

func (proc *ConsensusProcess) initDefaultBuilder(s *Set) *MessageBuilder {
	builder := NewMessageBuilder().SetPubKey(proc.pubKey).SetInstanceId(proc.instanceId)
	builder = builder.SetRoundCounter(proc.k).SetKi(proc.ki).SetValues(s)
	builder.SetRoleProof(proc.roleProof())

	return builder
}

func (proc *ConsensusProcess) buildNotifyMessage() *pb.HareMessage {
	builder := proc.initDefaultBuilder(proc.s).SetType(Notify).SetCertificate(proc.certificate).Sign(proc.signing)

	return builder.Build()
}

func (proc *ConsensusProcess) processPreRoundMsg(msg *pb.HareMessage) {
	proc.preRoundTracker.OnPreRound(msg)
}

func (proc *ConsensusProcess) processStatusMsg(msg *pb.HareMessage) {
	s := NewSet(msg.Message.Values)
	if proc.preRoundTracker.CanProveSet(s) {
		proc.statusesTracker.RecordStatus(msg)
	}
}

func (proc *ConsensusProcess) processProposalMsg(msg *pb.HareMessage) {
	// validate the proposed set is provable
	s := NewSet(msg.Message.Values)
	if !proc.preRoundTracker.CanProveSet(s) {
		log.Warning("Proposal validation failed: cannot prove set: %v", s)
		return
	}

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
	if ignored := proc.notifyTracker.OnNotify(msg); ignored {
		return
	}

	s := NewSet(msg.Message.Values)
	if proc.currentRound() == Round4 { // not necessary to update otherwise
		// update state
		if msg.Cert.AggMsgs.Messages[0].Message.Ki >= proc.ki { // we assume that the expression was checked before
			proc.s = s
			proc.certificate = msg.Cert
			proc.ki = msg.Message.Ki
		}
	}

	if proc.notifyTracker.NotificationsCount(s) < proc.cfg.F+1 { // not enough
		return
	}

	// enough notifications, should terminate
	log.Info("Consensus process terminated for %v with output set: ", proc.pubKey, proc.s)
	proc.terminating = true // ensures immediate termination
	proc.Close()
}

func (proc *ConsensusProcess) currentRound() int {
	return int(proc.k % 4)
}

func (proc *ConsensusProcess) endOfRound3() {
	if proc.proposalTracker.IsConflicting() {
		return
	}

	if !proc.commitTracker.HasEnoughCommits() {
		return
	}

	cert := proc.commitTracker.BuildCertificate()
	if cert == nil {
		log.Error("Build certificate returned nil")
		return
	}

	s := proc.proposalTracker.ProposedSet()
	if s == nil {
		return
	}
	proc.s = s
	proc.certificate = cert
	roundMsg := proc.buildNotifyMessage() // build notify with certificate
	proc.sendMessage(roundMsg)
}

func (proc *ConsensusProcess) updateRole() {
	proc.role = proc.oracle.Role(proc.roleProof())
}
