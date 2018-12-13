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
const InitialKnowledgeSize = 1000
const N = 800
const f = 400

type Byteable interface {
	Bytes() []byte
}

type NetworkService interface {
	RegisterProtocol(protocol string) chan service.Message
	Broadcast(protocol string, payload []byte) error
}

type State struct {
	k           uint32              // the iteration number
	ki          int32              // indicates when S was first committed upon
	s           *Set                // the set of blocks
	certificate *AggregatedMessages // the certificate
}

type ConsensusProcess struct {
	State
	Closer // the consensus is closeable
	pubKey          crypto.PublicKey
	layerId         LayerId
	t				*Set // loop local set
	oracle          Rolacle // roles oracle
	signing         Signing
	network         NetworkService
	startTime       time.Time // TODO: needed?
	inbox           chan *pb.HareMessage
	preRound		map[string]*pb.HareMessage
	knowledge       []*pb.HareMessage
	roundMsg        *pb.HareMessage
	isConflicting   bool
	isDecisionFinal bool
	tracker         *RefCountTracker
	// TODO: add knowledge of notify messages. What is the life-cycle of such messages? persistent according to Julian
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
	proc.preRound = make(map[string]*pb.HareMessage, N)
	proc.knowledge = make([]*pb.HareMessage, 0, InitialKnowledgeSize)
	proc.roundMsg = nil
	proc.tracker = NewRefCountTracker(N)
	proc.roundMsg = nil
	proc.isConflicting = false
	proc.isDecisionFinal = false

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

	timer := time.NewTimer(RoundDuration)
	PreRound:
	for {
		select {
		case msg := <-proc.inbox:
			proc.processMsgPreRound(msg)
		case <-timer.C:
			break PreRound
		}
	}

	// end of pre-round, update our set
	for _, v := range proc.s.blocks {
		if proc.tracker.CountStatus(v) < f + 1 { // not enough witnesses
			proc.s.Remove(v)
		}
	}

	// build and send pre-round message
	m, err := proc.buildPreRoundMessage()
	if err != nil {
		log.Error("could not build pre-round message")
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
	// Currently we reject past/future messages

	// verify iteration
	if m.Message.K != proc.k {
		log.Warning("Iteration mismatch. Message iteration is: %d expected: %d", m.Message.K, proc.k)
		return
	}

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

	if MessageType(m.Message.Type) == PreRound {
		// only handle first pre-round msg
		if _, exist := proc.preRound[pub.String()]; exist {
			return
		}

		proc.preRound[pub.String()] = m
	} else { // continue process msg by round
		proc.processMessageByRound(m)
	}

	proc.knowledge = append(proc.knowledge, m)
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

		proc.roundMsg = nil
	}

	// advance to next round
	proc.k++

	// reset knowledge
	proc.knowledge = make([]*pb.HareMessage, 0, InitialKnowledgeSize)
}

func (proc *ConsensusProcess) processMessageByRound(msg *pb.HareMessage) {
	if proc.isDecisionFinal { // no process required
		return
	}

	switch proc.k % 4 { // switch round
	case 0: // end of round 0
		proc.processMsgRound0(msg)
	case 1: // end of round 1
		proc.processMsgRound1(msg)
	case 2: // end of round 2
		proc.processMsgRound2(msg)
	case 3: // end of round 3
		proc.processMsgRound3(msg)
	}
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

	// TODO: try to build SVP based on msg

	//s := proc.tracker.buildSet(f+1)
	//builder.SetSVP(proc.knowledge)

	proc.roundMsg = builder.Build()

	return nil
}

func (proc *ConsensusProcess) setProposalMessage() error {
	builder := NewMessageBuilder()
	builder.SetType(Proposal).SetLayer(proc.layerId).SetIteration(proc.k).SetKi(proc.ki).SetBlocks(*proc.s)
	builder, err := builder.SetPubKey(proc.pubKey).Sign(proc.signing)

	if err != nil {
		return err
	}

	proc.roundMsg = builder.Build()

	return nil
}

func roleFromIteration(k uint32) byte {
	if k%4 == 1 { // only round 1 is leader
		return Leader
	}

	return Active
}

func (proc *ConsensusProcess) processMsgPreRound(msg *pb.HareMessage) {
	// TODO: validate sig
	// TODO: validate role

	pub, err := crypto.NewPublicKey(msg.PubKey)
	if err != nil {
		log.Warning("Could not construct public key: ", err.Error())
		return
	}

	// only handle first pre-round msg
	if _, exist := proc.preRound[pub.String()]; exist {
		return
	}

	// record blocks from msg
	s := NewSet(msg.Message.Blocks)
	for _, v := range s.blocks {
		proc.tracker.Track(v)
	}

	proc.preRound[pub.String()] = msg
}

func (proc *ConsensusProcess) processMsgRound0(msg *pb.HareMessage) {
	// TODO: roundMsg?

	// if certificate empty then simple proposal of my S with no certificate
	if proc.certificate == nil { // TODO: not good
		proc.setStatusMessage()
		return
	}

	// TODO: record blocks from msg


	if len(proc.knowledge) < f { // only record, wait for f+1 for decision
		return
	}

	proc.knowledge = append(proc.knowledge, msg)
	proc.setStatusMessage()
	proc.isDecisionFinal = true
}

func (proc *ConsensusProcess) processMsgRound1(msg *pb.HareMessage) {
	s := NewSet(msg.Message.Blocks)

	for i := 0; i < len(proc.knowledge); i++ {
		g := NewSet(proc.knowledge[i].Message.Blocks)
		if !s.Equals(g) {
			proc.isConflicting = true
			break
		}
	}

	proc.knowledge = append(proc.knowledge, msg)
	proc.State.s = s
	proc.setProposalMessage()
}

func (proc *ConsensusProcess) processMsgRound2(msg *pb.HareMessage) {
	s := NewSet(msg.Message.Blocks)

	count := 0
	for i := 0; i < len(proc.knowledge); i++ {
		g := NewSet(proc.knowledge[i].Message.Blocks)
		if s.Equals(g) {
			count++
		}
	}

	proc.knowledge = append(proc.knowledge, msg)

	if count == f+1 { //  we have enough commit messages on same S
		if !proc.isConflicting { // verify no contradiction
			proc.s = s // update s
			proc.certificate = NewAggregatedMessages(proc.knowledge, Signature{})
		}
	}
}

func (proc *ConsensusProcess) processMsgRound3(msg *pb.HareMessage) {
	// TODO: if received notify for S with certificate C
	// TODO: update state iff...
}
