package hare

import (
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"time"
)

const ProtoName = "HARE_PROTOCOL"
const RoundDuration = time.Second * time.Duration(15)
const N = 800
const f = 400
const layerSize = 200

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
	Closer // the consensus is closeable
	pubKey          crypto.PublicKey
	layerId         LayerId
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
}

func NewConsensusProcess(key crypto.PublicKey, layer LayerId, s Set, oracle Rolacle, signing Signing, p2p NetworkService) *ConsensusProcess {
	proc := &ConsensusProcess{}
	proc.State = State{0, 0, &s, nil}
	proc.Closer = NewCloser()
	proc.pubKey = key
	proc.layerId = layer
	proc.oracle = oracle
	proc.signing = signing
	proc.network = p2p
	proc.roundMsg = nil
	proc.preRoundTracker = NewPreRoundTracker(f+1)
	proc.statusesTracker = NewStatusTracker(f+1)
	proc.proposalTracker = NewProposalTracker()
	proc.commitTracker = NewCommitTracker()
	proc.notifyTracker = NewNotifyTracker()

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

	// build and send pre-round message
	m, err := proc.buildPreRoundMessage()
	if err != nil {
		log.Error("could not build pre-round message")
	}
	proc.network.Broadcast(ProtoName, m)

	// listen to pre-round messages
	timer := time.NewTimer(RoundDuration)
PreRound:
	for {
		select {
		case msg := <-proc.inbox:
			proc.handleMessage(msg)
		case <-timer.C:
			break PreRound
		}
	}

	// end of pre-round, update our set
	for _, v := range proc.s.blocks {
		if !proc.preRoundTracker.CanProveBlock(v) { // not enough witnesses
			proc.s.Remove(v)
		}
	}

	// set status to be sent in next round
	if err := proc.setStatusMessage(); err != nil {
		log.Error("Error setting status message: ", err.Error())
		proc.Close()
	}

	// send status message
	m, err = proto.Marshal(proc.roundMsg)
	if err != nil {
		log.Error("Could not marshal status message")
		proc.Close()
	}
	proc.network.Broadcast(ProtoName, m)

	// start first iteration
	ticker := time.NewTicker(RoundDuration)
	for {
		select {
		case msg := <-proc.inbox: // msg event
			proc.handleMessage(msg)
		case <-ticker.C: // next round event
			proc.nextRound()
		case <-proc.CloseChannel(): // close event
			log.Info("Stop event loop, instance aborted")
			return
		}
	}
}

func (proc *ConsensusProcess) handleMessage(m *pb.HareMessage) {
	// Note: layer is already verified by the broker
	// can receive any message at any point

	// validate signature
	data, err := proto.Marshal(m.Message)
	if err != nil {
		log.Error("Failed marshaling inner message")
		return
	}

	if !proc.signing.Validate(data, m.InnerSig) {
		log.Warning("invalid message signature detected")
		return
	}

	pub, err := crypto.NewPublicKey(m.PubKey)
	if err != nil {
		log.Warning("Could not construct public key: ", err.Error())
		return
	}

	// validate role
	if !proc.oracle.ValidateRole(roleFromIteration(m.Message.K),
		RoleRequest{pub, LayerId{NewBytes32(m.Message.Layer)}, m.Message.K},
		Signature(m.Message.RoleProof)) {
		log.Warning("invalid role detected")
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

func (proc *ConsensusProcess) nextRound() {
	log.Info("End of round: %d", proc.k)

	// check if message ready to send and check role to see if we should send
	if proc.roundMsg != nil && proc.oracle.ValidateRole(roleFromIteration(proc.k),
		RoleRequest{proc.pubKey, proc.layerId, proc.k},
		Signature{}) {
		m, err := proto.Marshal(proc.roundMsg)
		if err == nil { // no error, send msg
			proc.network.Broadcast(ProtoName, m)
		}
	}

	// reset trackers
	switch proc.k % 4 { // switch end of current round
	case 0:                                       // 0 is round 1
		proc.statusesTracker = NewStatusTracker(f+1) // reset statuses tracking
	case 2:                                         // 2 is round 3
		proc.proposalTracker = NewProposalTracker() // reset proposal tracking
		proc.commitTracker = NewCommitTracker()     // reset commits tracking
	}
	// TODO: check what to do with the notify. do we really need f+1 notify or can count on the certificate?

	// reset round message & iteration set
	proc.roundMsg = nil
	proc.t = nil

	// advance to next round
	proc.k++
}

func (proc *ConsensusProcess) buildPreRoundMessage() ([]byte, error) {
	builder := NewMessageBuilder()
	builder.SetType(PreRound).SetLayer(proc.layerId).SetIteration(proc.k).SetKi(proc.ki).SetBlocks(*proc.s)
	builder, err := builder.SetPubKey(proc.pubKey).Sign(proc.signing)

	if err != nil {
		return nil, err
	}

	m, err := proto.Marshal(builder.Build())
	if err != nil {
		return nil, err
	}

	return m, nil
}

func (proc *ConsensusProcess) setStatusMessage() error {
	builder := NewMessageBuilder()
	builder.SetType(Status).SetLayer(proc.layerId).SetIteration(proc.k).SetKi(proc.ki).SetBlocks(*proc.s)
	builder, err := builder.SetPubKey(proc.pubKey).Sign(proc.signing)

	if err != nil {
		return err
	}

	proc.roundMsg = builder.Build()

	return nil
}

func (proc *ConsensusProcess) setProposalMessage(svp *pb.AggregatedMessages) error {
	builder := NewMessageBuilder()
	builder.SetType(Proposal).SetLayer(proc.layerId).SetIteration(proc.k).SetKi(proc.ki).SetBlocks(*proc.s)
	builder.SetSVP(svp)
	builder, err := builder.SetPubKey(proc.pubKey).Sign(proc.signing)

	if err != nil {
		return err
	}

	proc.roundMsg = builder.Build()

	return nil
}

func (proc *ConsensusProcess) buildNotifyMessage() (*pb.HareMessage, error) {
	builder := NewMessageBuilder()
	builder.SetType(Proposal).SetLayer(proc.layerId).SetIteration(proc.k).SetKi(proc.ki).SetBlocks(*proc.s)
	builder.SetCertificate(proc.certificate)
	builder, err := builder.SetPubKey(proc.pubKey).Sign(proc.signing)

	if err != nil {
		return nil, err
	}

	return builder.Build(), nil
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

	if !proc.proposalTracker.HasValidProposal() {
		proc.t = nil
		return
	}

	*proc.t = proc.proposalTracker.ProposedSet()
}

func (proc *ConsensusProcess) processCommitMsg(msg *pb.HareMessage) {
	if proc.proposalTracker.isConflicting {
		proc.roundMsg = nil
		return
	}

	if !proc.commitTracker.HasEnoughCommits() {
		return
	}

	proc.s = proc.t // commit to t
	proc.certificate = proc.commitTracker.BuildCertificate()
	notifyMsg, err := proc.buildNotifyMessage() // build notify with certificate

	if err != nil {
		log.Warning("Could not build notify message: ", err)
		return
	}

	proc.roundMsg = notifyMsg
}

func (proc *ConsensusProcess) processNotifyMsg(msg *pb.HareMessage) {
	proc.notifyTracker.OnNotify(msg)

	// TODO: consider doing it only on change ?
	m := proc.notifyTracker.GetNotifyMsg()
	proc.s = NewSet(m.Message.Blocks)
	proc.certificate = m.Cert
}
