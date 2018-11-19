package hare

import (
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
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
	// TODO: add knowledge of notify messages. What is the life-cycle of such messages?
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

	// validate role TODO: in proposal we have additional verification at the end of the round (in processMessages)
	if !proc.oracle.ValidateRole(roleFromIteration(m.Message.K),
		RoleRequest{PubKey{NewBytes32(m.PubKey)}, LayerId{NewBytes32(m.Message.Layer)}, m.Message.K},
		Signature(m.Message.RoleProof)) {
		return
	}

	proc.knowledge = append(proc.knowledge, m)
}

func (proc *ConsensusProcess) nextRound() {
	log.Info("Next round: %d", proc.k+1)

	// process the round
	go proc.processMessages(proc.knowledge, proc.k)

	// advance to next round
	proc.k++

	// reset knowledge
	proc.knowledge = make([]*pb.HareMessage, InitialKnowledgeSize)
}

// TODO: put msg []*pb.HareMessage, k uint32 in Context struct?
func (proc *ConsensusProcess) processMessages(msg []*pb.HareMessage, k uint32) {
	// TODO: safety?
	// check if process of messages has already been made for this round
	if _, exist := proc.isProcessed[k]; exist {
		return
	}

	// TODO: process to create suitable hare message

	m, err := proc.lookForProposal(msg, k) // end of round 0
	if err != nil {
		return
	}

	// send message
	proc.network.Broadcast(ProtoName, m)

	// mark as processed
	proc.isProcessed[k] = true
}

func (proc *ConsensusProcess) lookForProposal(msg []*pb.HareMessage, k uint32) ([]byte, error) {

	// TODO: pass on knowledge & look for a set to propose

	x := &pb.InnerMessage{}
	x.Type = int32(Proposal)
	x.Layer = proc.layerId.Bytes()
	x.K = k
	x.Ki = proc.ki
	x.Blocks = proc.s.To2DSlice()
	x.RoleProof = proc.oracle.Role(RoleRequest{proc.pubKey, proc.layerId, k})
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
