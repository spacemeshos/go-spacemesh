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
	s           *Set            // the set of blocks
	certificate *pb.Certificate // the certificate
}

type ConsensusProcess struct {
	State
	Closer          // the consensus is closeable
	pubKey          crypto.PublicKey
	layerId         SetId
	t               *Set    // loop local set
	oracle          Rolacle // roles oracle
	signing         Signing
	network         NetworkService
	startTime       time.Time // TODO: needed?
	inbox           chan *pb.HareMessage
	roundMsg        *pb.HareMessage
	preRoundTracker PreRoundTracker
	statusesTracker StatusTracker
	proposalTracker ProposalTracker
	commitTracker   CommitTracker
	notifyTracker   NotifyTracker
	terminating     bool
	cfg             config.Config
}

func NewConsensusProcess(cfg config.Config, key crypto.PublicKey, layer SetId, s Set, oracle Rolacle, signing Signing, p2p NetworkService) *ConsensusProcess {
	proc := &ConsensusProcess{}
	proc.State = State{0, -1, &s, nil}
	proc.Closer = NewCloser()
	proc.pubKey = key
	proc.layerId = layer
	proc.oracle = oracle
	proc.signing = signing
	proc.network = p2p
	proc.roundMsg = nil
	proc.preRoundTracker = NewPreRoundTracker(cfg.F+1, cfg.N)
	proc.statusesTracker = NewStatusTracker(cfg.F+1, cfg.N)
	proc.proposalTracker = NewProposalTracker(cfg.N)
	proc.commitTracker = NewCommitTracker(cfg.F+1, cfg.N)
	proc.notifyTracker = NewNotifyTracker(cfg.N)
	proc.terminating = false
	proc.cfg = cfg

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
	proc.setPreRoundMessage()
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

	// set status and send
	proc.setStatusMessage()
	proc.sendPendingMessage()

	// start first iteration
	ticker := time.NewTicker(proc.cfg.RoundDuration)
	for {
		select {
		case msg := <-proc.inbox: // msg event
			proc.handleMessage(msg)
		case <-ticker.C: // next round event
			proc.nextRound()
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
	currentRound := proc.k % 4

	switch MessageType(m.Message.Type) {
	case PreRound:
		return true
	case Status:
		return currentRound == 1
	case Proposal:
		return currentRound == 2 || currentRound == 3
	case Notify:
		return true
	}

	log.Error("Unknown message type encountered during validation: ", m.Message.Type)
	return false
}

func (proc *ConsensusProcess) validateCertificate(m *pb.HareMessage) bool {
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

func (proc *ConsensusProcess) handleMessage(m *pb.HareMessage) {
	// Note: layer is already verified by the broker

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
	if !proc.oracle.ValidateRole(roleFromIteration(m.Message.K),
		RoleRequest{pub, SetId{NewBytes32(m.Message.Layer)}, m.Message.K},
		Signature(m.Message.RoleProof)) {
		log.Warning("invalid role detected")
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

	// validate certificate (only notify has certificate)
	if MessageType(m.Message.Type) == Notify && !proc.validateCertificate(m) {
		log.Warning("invalid certificate detected")
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
	if !proc.oracle.ValidateRole(roleFromIteration(proc.k),
		RoleRequest{proc.pubKey, proc.layerId, proc.k},
		Signature{}) {
		return
	}

	data, err := proto.Marshal(proc.roundMsg)
	if err != nil {
		panic("could not marshal message before send")
	}

	proc.network.Broadcast(ProtoName, data)
}

func (proc *ConsensusProcess) nextRound() {
	log.Info("End of round: %d", proc.k)

	proc.sendPendingMessage()

	// reset trackers
	switch proc.k % 4 { // switch end of current round
	case 0: // 0 is round 1
		proc.statusesTracker = NewStatusTracker(proc.cfg.F+1, proc.cfg.N) // reset statuses tracking
	case 2: // 2 is round 3
		proc.proposalTracker = NewProposalTracker(proc.cfg.N)  // reset proposal tracking
		proc.commitTracker = NewCommitTracker(proc.cfg.F+1, proc.cfg.N) // reset commits tracking
	}
	// TODO: check what to do with the notify. do we really need f+1 notify or can count on the certificate?

	// reset round message & iteration set
	proc.roundMsg = nil
	proc.t = nil

	// advance to next round
	proc.k++
}

func (proc *ConsensusProcess) setPreRoundMessage() {
	builder := NewMessageBuilder()
	builder.SetType(PreRound).SetLayer(proc.layerId).SetIteration(proc.k).SetKi(proc.ki).SetValues(proc.s)
	builder = builder.SetPubKey(proc.pubKey).Sign(proc.signing)

	proc.roundMsg = builder.Build()
}

func (proc *ConsensusProcess) setStatusMessage() {
	builder := NewMessageBuilder()
	builder.SetType(Status).SetLayer(proc.layerId).SetIteration(proc.k).SetKi(proc.ki).SetValues(proc.s)
	builder = builder.SetPubKey(proc.pubKey).Sign(proc.signing)

	proc.roundMsg = builder.Build()
}

func (proc *ConsensusProcess) setProposalMessage(svp *pb.AggregatedMessages) {
	builder := NewMessageBuilder()
	builder.SetType(Proposal).SetLayer(proc.layerId).SetIteration(proc.k).SetKi(proc.ki).SetValues(proc.statusesTracker.BuildUnionSet(proc.cfg.SetSize))
	builder.SetSVP(svp)
	builder = builder.SetPubKey(proc.pubKey).Sign(proc.signing)

	proc.roundMsg = builder.Build()
}

func (proc *ConsensusProcess) buildNotifyMessage() *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(Proposal).SetLayer(proc.layerId).SetIteration(proc.k).SetKi(proc.ki).SetValues(proc.s)
	builder.SetCertificate(proc.certificate)
	builder = builder.SetPubKey(proc.pubKey).Sign(proc.signing)

	return builder.Build()
}

func roleFromIteration(k uint32) byte {
	if k%4 == 1 { // only round 1 is leader
		return Leader
	}

	return Active
}

func (proc *ConsensusProcess) processPreRoundMsg(msg *pb.HareMessage) {
	proc.preRoundTracker.OnPreRound(msg)
}

func (proc *ConsensusProcess) processStatusMsg(msg *pb.HareMessage) {
	s := NewSet(msg.Message.Blocks)
	if proc.preRoundTracker.CanProveSet(s) {
		proc.statusesTracker.RecordStatus(msg)
	}

	if proc.statusesTracker.IsSVPReady() {
		proc.setProposalMessage(proc.statusesTracker.BuildSVP())
	}
}

func (proc *ConsensusProcess) processProposalMsg(msg *pb.HareMessage) {
	proc.proposalTracker.OnProposal(msg)

	set, ok := proc.proposalTracker.ProposedSet()
	if !ok {
		proc.t = nil
		return
	}

	proc.t = set
}

func (proc *ConsensusProcess) processCommitMsg(msg *pb.HareMessage) {
	if proc.proposalTracker.IsConflicting() {
		proc.roundMsg = nil
		return
	}

	if !proc.commitTracker.HasEnoughCommits() {
		return
	}

	proc.s = proc.t // commit to t
	proc.certificate = proc.commitTracker.BuildCertificate()
	proc.roundMsg = proc.buildNotifyMessage() // build notify with certificate
}

func (proc *ConsensusProcess) processNotifyMsg(msg *pb.HareMessage) {
	if exist := proc.notifyTracker.OnNotify(msg); exist {
		return
	}

	s := NewSet(msg.Message.Blocks)
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
