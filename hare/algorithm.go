package hare

import (
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"sync"
	"time"
)

const ProtoName = "HARE_PROTOCOL"
const RoundDuration = time.Second * time.Duration(15)
const InitialKnowledgeSize = 1000

type Byteable interface {
	Bytes() []byte
}

type NetworkService interface {
	RegisterProtocol(protocol string) chan service.Message
	Broadcast(protocol string, payload []byte) error
}

type State struct {
	k    uint32      // the iteration number
	ki   uint32      // ?
	s    Set         // the set of blocks
	cert Certificate // the certificate
}

type ConsensusProcess struct {
	State
	Closer // the consensus is closeable
	pubKey      PubKey
	layerId     LayerId
	oracle      Rolacle // roles oracle
	signing     Signing
	network     NetworkService
	startTime   time.Time // TODO: needed?
	inbox       chan *pb.HareMessage
	knowledge   []*pb.HareMessage
	isProcessed map[uint32]bool // TODO: could be empty struct
	mutex       sync.Mutex
	// TODO: add knowledge of notify messages. What is the life-cycle of such messages? persistent according to Julian
}

func (proc *ConsensusProcess) NewConsensusProcess(layer LayerId, s Set, oracle Rolacle, signing Signing, p2p NetworkService, broker *Broker) {
	proc.State = State{0, 0, s, Certificate{}}
	proc.Closer = NewCloser()
	proc.layerId = layer
	proc.oracle = oracle
	proc.signing = signing
	proc.network = p2p
	proc.inbox = broker.CreateInbox(&layer)
	proc.knowledge = make([]*pb.HareMessage, 0)
	proc.isProcessed = make(map[uint32]bool)
}

func (proc *ConsensusProcess) Start() {
	if !proc.startTime.IsZero() { // called twice on same instance
		log.Error("ConsensusProcess has already been started.")
		return
	}

	proc.startTime = time.Now()

	// send first status message
	m, err := proc.buildStatusMessage()
	if err == nil {
		proc.network.Broadcast(ProtoName, m)
	}

	go proc.eventLoop()
}

func (proc *ConsensusProcess) WaitForCompletion() {
	select {
	case <-proc.CloseChannel():
		// TODO: access broker and remove layer (consider register/unregister terminology)
		return
	}
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
		return
	}
	if !proc.signing.Validate(data, m.InnerSig) {
		return
	}

	// validate role
	if !proc.oracle.ValidateRole(roleFromIteration(m.Message.K),
		RoleRequest{PubKey{NewBytes32(m.PubKey)}, LayerId{NewBytes32(m.Message.Layer)}, m.Message.K},
		Signature(m.Message.RoleProof)) {
		return
	}

	proc.knowledge = append(proc.knowledge, m)
}

func (proc *ConsensusProcess) nextRound() {
	log.Info("End of round: %d", proc.k)

	// process the round
	// state is passed by value, ownership of knowledge is also passed
	go proc.processMessages(proc.State, proc.knowledge)

	// advance to next round
	proc.k++

	// can reset knowledge as ownership was passed before
	proc.knowledge = make([]*pb.HareMessage, InitialKnowledgeSize)
}

// TODO: put state and msg in Context struct?
func (proc *ConsensusProcess) processMessages(state State, msg []*pb.HareMessage) {
	// only one process should run concurrently
	proc.mutex.Lock()
	defer proc.mutex.Unlock()

	// check if process of messages has already been made for this round
	if _, exist := proc.isProcessed[state.k]; exist {
		return
	}

	// TODO: in proposal we should also verify there is no contradiction

	// TODO: process to create suitable hare message for the round

	m, err := proc.lookForProposal(state, msg) // end of round 0
	if err != nil {
		return
	}

	// send message
	proc.network.Broadcast(ProtoName, m)

	// mark as processed
	proc.isProcessed[state.k] = true
}

func (proc *ConsensusProcess) buildStatusMessage() ([]byte, error) {
	x := &pb.InnerMessage{}
	x.Type = int32(Status)
	x.Layer = proc.layerId.Bytes()
	x.K = proc.k
	x.Ki = proc.ki
	x.Blocks = proc.s.To2DSlice()

	buff, err := proto.Marshal(x)
	if err != nil {
		return nil, errors.New("error marshaling inner message")
	}

	sig := proc.signing.Sign(buff)

	m := &pb.HareMessage{}
	m.PubKey = proc.pubKey.Bytes()
	m.Message = x
	m.InnerSig = sig

	buff, err = proto.Marshal(m)
	if err != nil {
		return nil, errors.New("error marshaling message")
	}

	return buff, nil
}

func (proc *ConsensusProcess) lookForProposal(state State, msg []*pb.HareMessage) ([]byte, error) {

	// TODO: pass on msg & look for a set to propose

	x := &pb.InnerMessage{}
	x.Type = int32(Proposal)
	x.Layer = proc.layerId.Bytes()
	x.K = state.k
	x.Ki = state.ki
	x.Blocks = state.s.To2DSlice()
	x.RoleProof = proc.oracle.Role(RoleRequest{proc.pubKey, proc.layerId, state.k})
	x.Svp = &pb.AggregatedMessages{} // TODO: build SVP msg

	buff, err := proto.Marshal(x)
	if err != nil {
		return nil, errors.New("error marshaling inner message")
	}

	sig := proc.signing.Sign(buff)

	m := &pb.HareMessage{}
	m.PubKey = proc.pubKey.Bytes()
	m.Message = x
	m.InnerSig = sig
	// TODO: m.Cert =

	buff, err = proto.Marshal(m)
	if err != nil {
		return nil, errors.New("error marshaling message")
	}

	return buff, nil
}

func roleFromIteration(k uint32) byte {
	if k%4 == 1 { // only round 1 is leader
		return Leader
	}

	return Active
}
