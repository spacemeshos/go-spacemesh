package hare

import (
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
	k           uint32          // the iteration number
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
	roundMsg        *pb.HareMessage
	role            RolacleResponse // the current role
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
	proc.roundMsg = nil
	proc.preRoundTracker = NewPreRoundTracker(cfg.F+1, cfg.N)
	proc.statusesTracker = NewStatusTracker(cfg.F+1, cfg.N)
	proc.proposalTracker = NewProposalTracker(cfg.N)
	proc.commitTracker = NewCommitTracker(cfg.F+1, cfg.N, nil)
	proc.notifyTracker = NewNotifyTracker(cfg.N)
	proc.terminating = false
	proc.cfg = cfg

	proc.updateRole()

	return proc
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

	// set pre-round message and send
	proc.roundMsg = proc.initDefaultBuilder(proc.s).SetType(PreRound).Sign(proc.signing).Build()
	proc.sendPendingMessage()

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

func (proc *ConsensusProcess) doesMessageMatchRound(m *pb.HareMessage) bool {
	currentRound := proc.currentRound()

	switch MessageType(m.Message.Type) {
	case PreRound:
		return true
	case Status:
		return currentRound == 0
	case Proposal:
		return currentRound == 1 || currentRound == 2
	case Commit:
		return currentRound == 2
	case Notify:
		return true
	}

	// TODO: should be verified before and panic here
	log.Error("Unknown message type encountered during validation: ", m.Message.Type)
	return false
}

func (proc *ConsensusProcess) validateCertificate(m *pb.HareMessage) bool {
	if m == nil {
		return false
	}

	if m.Cert == nil {
		return false
	}

	if m.Cert.AggMsgs == nil {
		return false
	}

	if m.Cert.AggMsgs.Messages == nil {
		return false
	}

	if len(m.Cert.AggMsgs.Messages) == 0 {
		return false
	}

	// TODO: validate agg sig

	return true
}

func roleFromIteration(k uint32) Role {
	if k%4 == Round2 {
		return Leader
	}

	return Active
}

func (proc *ConsensusProcess) handleMessage(m *pb.HareMessage) {
	// Note: set id is already verified by the broker

	// validate round and message type match
	if !proc.doesMessageMatchRound(m) {
		log.Info("Message does not match the current round")
		return
	}

	pub, err := crypto.NewPublicKey(m.PubKey)
	if err != nil {
		log.Warning("Could not construct public key: ", err.Error())
		return
	}

	// validate role
	request := RoleRequest{pub, InstanceId{NewBytes32(m.Message.InstanceId)}, m.Message.K}
	response := RolacleResponse{roleFromIteration(m.Message.K), Signature(m.Message.RoleProof)}
	if !proc.oracle.ValidateRole(request, response) {
		log.Warning("invalid role detected for: ", m.String())
		return
	}

	data, err := proto.Marshal(m.Message)
	if err != nil {
		log.Error("Failed marshaling inner message")
		return
	}

	// validate signature
	if !proc.signing.Validate(data, m.InnerSig) {
		log.Warning("invalid message signature detected")
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

func (proc *ConsensusProcess) sendPendingMessage() {
	// no message ready
	if proc.roundMsg == nil {
		return
	}

	// validate role
	request := RoleRequest{proc.pubKey, InstanceId{NewBytes32(proc.instanceId.Bytes())}, proc.k}
	response := RolacleResponse{proc.role.Role, proc.role.Signature}
	if !proc.oracle.ValidateRole(request, response) {
		return
	}

	data, err := proto.Marshal(proc.roundMsg)
	if err != nil {
		log.Error("failed marshaling message")
		panic("could not marshal message before send")
	}

	proc.network.Broadcast(ProtoName, data)
	proc.roundMsg = nil // sent
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
	proc.updateRole()
}

func (proc *ConsensusProcess) beginRound1() {
	proc.roundMsg = proc.initDefaultBuilder(proc.s).SetType(Status).Sign(proc.signing).Build()
	proc.statusesTracker = NewStatusTracker(proc.cfg.F+1, proc.cfg.N)
}

func (proc *ConsensusProcess) beginRound2() {
	if proc.statusesTracker.IsSVPReady() {
		builder := proc.initDefaultBuilder(proc.statusesTracker.BuildUnionSet(proc.cfg.SetSize))
		svp := proc.statusesTracker.BuildSVP()
		if svp != nil {
			proc.roundMsg = builder.SetType(Proposal).SetSVP(svp).Sign(proc.signing).Build()
		}
	}

	// done with building proposal, reset statuses tracking
	proc.statusesTracker = nil
}

func (proc *ConsensusProcess) beginRound3() {
	proposedSet := proc.proposalTracker.ProposedSet()
	if proposedSet != nil { // has proposal to send
		builder := proc.initDefaultBuilder(proposedSet).SetType(Notify).SetCertificate(proc.certificate).Sign(proc.signing)
		proc.roundMsg = builder.Build()
	}
	proc.commitTracker = NewCommitTracker(proc.cfg.F+1, proc.cfg.N, proposedSet) // track commits for proposed T
}

func (proc *ConsensusProcess) beginRound4() {
	proc.commitTracker = nil
	proc.proposalTracker = nil
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
	}

	// send pending message
	proc.sendPendingMessage()
}

func (proc *ConsensusProcess) initDefaultBuilder(s *Set) *MessageBuilder {
	builder := NewMessageBuilder().SetPubKey(proc.pubKey).SetInstanceId(proc.instanceId)
	builder = builder.SetIteration(proc.k).SetKi(proc.ki).SetValues(s)

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
	// TODO: validate SVP

	if proc.currentRound() == Round2 { // regular proposal
		proc.proposalTracker.OnProposal(msg)
	} else { // late proposal
		proc.proposalTracker.OnLateProposal(msg)
	}
}

func (proc *ConsensusProcess) processCommitMsg(msg *pb.HareMessage) {
	proc.commitTracker.OnCommit(msg)
}

// TODO: I think we should prepare the status message also here
func (proc *ConsensusProcess) processNotifyMsg(msg *pb.HareMessage) {
	// validate certificate (only notify has certificate)
	if !proc.validateCertificate(msg) {
		log.Warning("invalid certificate detected")
		return
	}

	if ignored := proc.notifyTracker.OnNotify(msg); ignored {
		return
	}

	s := NewSet(msg.Message.Values)
	// we assume that the expression was checked on handleMessage
	if msg.Cert.AggMsgs.Messages[0].Message.Ki >= proc.ki {
		proc.s = s
		proc.certificate = msg.Cert
		proc.ki = msg.Message.Ki
	}

	if proc.notifyTracker.NotificationsCount(s) < proc.cfg.F+1 { // not enough
		return
	}

	// enough notifications, should broadcast & terminate
	notifyMsg := proc.buildNotifyMessage()
	data, err := proto.Marshal(notifyMsg)
	if err != nil {
		log.Error("Could not marshal notify message")
		proc.Close()
	}
	proc.network.Broadcast(ProtoName, data)
	proc.terminating = true // ensures immediate termination
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

	proc.s = proc.proposalTracker.ProposedSet() // commit to t
	proc.certificate = cert
	proc.roundMsg = proc.buildNotifyMessage() // build notify with certificate
	proc.sendPendingMessage()
}
func (proc *ConsensusProcess) updateRole() {
	proc.role = proc.oracle.Role(RoleRequest{proc.pubKey, proc.instanceId, proc.k})
}
