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
	stateMutex  sync.Mutex
	// TODO: add knowledge of notify messages. What is the life-cycle of such messages? persistent according to Julian
}

func NewConsensusProcess(key PubKey, layer LayerId, s Set, oracle Rolacle, signing Signing, p2p NetworkService, broker *Broker) *ConsensusProcess {
	proc := &ConsensusProcess{}
	proc.State = State{0, 0, s, Certificate{}}
	proc.Closer = NewCloser()
	proc.pubKey = key
	proc.layerId = layer
	proc.oracle = oracle
	proc.signing = signing
	proc.network = p2p
	proc.inbox = broker.CreateInbox(&layer)
	proc.knowledge = make([]*pb.HareMessage, 0)
	proc.isProcessed = make(map[uint32]bool)

	return proc
}

func (proc *ConsensusProcess) Start() error {
	if !proc.startTime.IsZero() { // called twice on same instance
		log.Error("ConsensusProcess has already been started.")
		return errors.New("failed starting consensus process")
	}

	proc.startTime = time.Now()

	// send first status message
	m, err := proc.buildStatusMessage()
	if err != nil {
		return errors.New("failed starting consensus process")
	}

	proc.network.Broadcast(ProtoName, m)

	go proc.eventLoop()

	return nil
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
func (proc *ConsensusProcess) processMessages(state State, msgs []*pb.HareMessage) {
	// only one process should run concurrently
	proc.mutex.Lock()
	defer proc.mutex.Unlock()

	// check if process of messages has already been made for this round
	if _, exist := proc.isProcessed[state.k]; exist {
		return
	}

	// mark as processed
	proc.isProcessed[state.k] = true

	// TODO: process to create suitable hare message for the round

	var m []byte
	var err error = nil
	switch state.k % 4 {
	case 0: // end of round 0
		m, err = proc.lookForProposal(state, msgs)
	case 1: // end of round 1
		m, err = proc.validateProposal(state, msgs)
	case 2: // end of round 2
		m, err = proc.lookForCommits(state, msgs)
	case 3: // end of round 3
		m, err = proc.buildStatusMessage()
	}

	if err != nil {
		return
	}

	// send message
	proc.network.Broadcast(ProtoName, m)
}

func (proc *ConsensusProcess) buildStatusMessage() ([]byte, error) {
	x := NewInnerBuilder().SetType(Status).SetLayer(proc.layerId).SetIteration(proc.k).SetKi(proc.ki).SetBlocks(proc.s).Build()

	buff, err := proto.Marshal(x)
	if err != nil {
		return nil, errors.New("error marshaling inner message")
	}

	sig := proc.signing.Sign(buff)

	outer := NewProtoBuilder()
	m := outer.SetInnerMessage(x).SetPubKey(proc.pubKey).SetInnerSignature(sig).Build()

	buff, err = proto.Marshal(m)
	if err != nil {
		return nil, errors.New("error marshaling message")
	}

	return buff, nil
}

func (proc *ConsensusProcess) lookForProposal(state State, msg []*pb.HareMessage) ([]byte, error) {

	// TODO: pass on msg & look for a set to propose

	inner := NewInnerBuilder()
	inner.SetType(Proposal).SetLayer(proc.layerId).SetIteration(proc.k).SetKi(proc.ki).SetBlocks(proc.s)
	inner.SetRoleProof(proc.oracle.Role(RoleRequest{proc.pubKey, proc.layerId, state.k}))

	x := inner.Build()

	buff, err := proto.Marshal(x)
	if err != nil {
		return nil, errors.New("error marshaling inner message")
	}

	sig := proc.signing.Sign(buff)

	outer := NewProtoBuilder()
	outer.SetPubKey(proc.pubKey).SetInnerMessage(x).SetInnerSignature(sig)

	buff, err = proto.Marshal(outer.Build())
	if err != nil {
		return nil, errors.New("error marshaling message")
	}

	return buff, nil
}

func (proc *ConsensusProcess) validateProposal(state State, msg []*pb.HareMessage) ([]byte, error) {
	// TODO: validate no contradicting proposals

	// TODO: if ok then SAFELY update state

	// TODO: if ok then build commit message

	return []byte{}, nil
}

func (proc *ConsensusProcess) lookForCommits(state State, msg []*pb.HareMessage) ([]byte, error) {
	// TODO: check if we received f+1 commit messages

	// TODO: if ok construct the notify message

	return []byte{}, nil
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
