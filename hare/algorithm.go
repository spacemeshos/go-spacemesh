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
const f = 100

type Byteable interface {
	Bytes() []byte
}

type NetworkService interface {
	RegisterProtocol(protocol string) chan service.Message
	Broadcast(protocol string, payload []byte) error
}

type State struct {
	k    uint32       // the iteration number
	ki   uint32       // indicates when S was first committed upon
	s    *Set         // the set of blocks
	cert *Certificate // the certificate
}

type ConsensusProcess struct {
	State
	Closer // the consensus is closeable
	pubKey        crypto.PublicKey
	layerId       LayerId
	oracle        Rolacle // roles oracle
	signing       Signing
	network       NetworkService
	startTime     time.Time // TODO: needed?
	inbox         chan *pb.HareMessage
	knowledge     []*pb.HareMessage
	roundMsg      *pb.HareMessage
	isConflicting bool
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
	proc.knowledge = make([]*pb.HareMessage, 0, InitialKnowledgeSize)
	proc.roundMsg = nil
	err := proc.setStatusMessage()
	if err != nil {
		log.Error("could not build status message")
		proc.roundMsg = nil
	}
	proc.isConflicting = false

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

	// validate role
	if !proc.oracle.ValidateRole(roleFromIteration(m.Message.K),
		RoleRequest{m.PubKey, LayerId{NewBytes32(m.Message.Layer)}, m.Message.K},
		Signature(m.Message.RoleProof)) {
		log.Warning("invalid role detected")
		return
	}

	// continue process msg by round
	proc.processMessageByRound(m)

	// TODO: decide how we store messages maybe we don't need raw msgs
	proc.knowledge = append(proc.knowledge, m)
}

func (proc *ConsensusProcess) nextRound() {
	log.Info("End of round: %d", proc.k)

	if proc.roundMsg != nil { // message available
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

// TODO: put state and msg in Context struct?
func (proc *ConsensusProcess) processMessageByRound(msg *pb.HareMessage) {
	// TODO: check MY role, check if already processed

	// TODO: do we have cases in which we don't need to do further processing? (final decisions)
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

func (proc *ConsensusProcess) processMsgRound0(msg *pb.HareMessage) {
	// TODO: if cert empty then simple proposal of my S with no cert

	//proc.mymap[msg]

	if proc.cert == nil {
		proc.setStatusMessage()
	}

	// TODO: try to build SVP based on msg
	//s := NewSet(msg.Message.Blocks)

	// TODO: update state to prepare proposal message
	// TODO: if have svp build proposal msg
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
			buildCertificate(s, proc.knowledge)
		}
	}
}

func (proc *ConsensusProcess) processMsgRound3(msg *pb.HareMessage) {
	// TODO: if received notify for S with cert C
	// TODO: update state iff...
}
