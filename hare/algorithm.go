package hare

import (
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"sync"
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
	k    uint32      // the iteration number
	ki   uint32      // indicates when S was first committed upon
	s    Set         // the set of blocks
	cert Certificate // the certificate
}

type ConsensusProcess struct {
	State
	Closer // the consensus is closeable
	pubKey      crypto.PublicKey
	layerId     LayerId
	oracle      Rolacle // roles oracle
	signing     Signing
	network     NetworkService
	startTime   time.Time // TODO: needed?
	inbox       chan *pb.HareMessage
	knowledge   []*pb.HareMessage
	stateMutex  sync.Mutex
	roundMsg    []byte
	// TODO: add knowledge of notify messages. What is the life-cycle of such messages? persistent according to Julian
}

func NewConsensusProcess(key crypto.PublicKey, layer LayerId, s Set, oracle Rolacle, signing Signing, p2p NetworkService) *ConsensusProcess {
	proc := &ConsensusProcess{}
	proc.State = State{0, 0, s, Certificate{}}
	proc.Closer = NewCloser()
	proc.pubKey = key
	proc.layerId = layer
	proc.oracle = oracle
	proc.signing = signing
	proc.network = p2p
	proc.knowledge = make([]*pb.HareMessage, 0, InitialKnowledgeSize)
	m, err := proc.buildStatusMessage()
	if err != nil {
		log.Error("could not build status message")
		proc.roundMsg = nil
	} else {
		proc.roundMsg = m
	}

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

	// verify iteration
	if m.Message.K != proc.k {
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

	// send message if available
	if proc.roundMsg != nil {
		proc.network.Broadcast(ProtoName, proc.roundMsg)
		proc.roundMsg = nil
	}

	// advance to next round
	proc.k++

	// reset knowledge
	proc.knowledge = make([]*pb.HareMessage, 0, InitialKnowledgeSize)
}

// TODO: put state and msg in Context struct?
func (proc *ConsensusProcess) processMessageByRound(msg *pb.HareMessage) {
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

// TODO: maybe change it to SetStatusMessage and just set proc.roundMsg?
func (proc *ConsensusProcess) buildStatusMessage() ([]byte, error) {
	x := NewInnerBuilder().SetType(Status).SetLayer(proc.layerId).SetIteration(proc.k).SetKi(proc.ki).SetBlocks(proc.s).Build()

	buff, err := proto.Marshal(x)
	if err != nil {
		return nil, errors.New("error marshaling inner message")
	}

	sig := proc.signing.Sign(buff)

	outer := NewOuterBuilder()
	m := outer.SetInnerMessage(x).SetPubKey(proc.pubKey).SetInnerSignature(sig).Build()

	buff, err = proto.Marshal(m)
	if err != nil {
		return nil, errors.New("error marshaling message")
	}

	return buff, nil
}

func (proc *ConsensusProcess) updateState(state State) {
	proc.stateMutex.Lock()
	proc.State = state
	proc.stateMutex.Unlock()
}

func roleFromIteration(k uint32) byte {
	if k%4 == 1 { // only round 1 is leader
		return Leader
	}

	return Active
}

func (proc *ConsensusProcess) processMsgRound0(msg *pb.HareMessage) {
	// TODO: if cert empty then simple proposal of my S with no cert
	// TODO: try to build SVP based on msg
	// TODO: update state to prepare proposal message
	// TODO: if have svp build proposal msg
}

func (proc *ConsensusProcess) processMsgRound1(msg *pb.HareMessage) {
	// TODO: verify no contradiction with current proposal
	// TODO: update state - state.S=S else state.S=nil
}

func (proc *ConsensusProcess) processMsgRound2(msg *pb.HareMessage) {
	// TODO: check if you have f+1 commit messages on same S
	// TODO: if ok - update state.S=S
	// TODO: build a certificate C from the messages
}

func (proc *ConsensusProcess) processMsgRound3(msg *pb.HareMessage) {
	// TODO: if received notify for S with cert C
	// TODO: update state iff...
}
