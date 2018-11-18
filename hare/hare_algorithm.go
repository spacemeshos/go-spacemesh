package hare

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"hash/fnv"
	"time"
)

const ProtoName = "HARE_PROTOCOL"
const RoundDuration = time.Second * time.Duration(15)
const InitialKnowledgeSize = 1000

type PubKey [20]byte
type RoleSignature [25]byte
type BlockId uint32   // TODO: replace with import
type LayerId [32]byte // TODO: replace with import

type Byteable interface {
	Bytes() []byte
}

type NetworkService interface {
	RegisterProtocol(protocol string) chan service.Message
	Broadcast(protocol string, payload []byte) error
}

type Identifiable interface {
	Id() uint32
}

func NewLayerId(buff []byte) *LayerId {
	layer := &LayerId{}
	copy(layer.Bytes(), buff)

	return layer
}

func (layerId *LayerId) Id() uint32 {
	h := fnv.New32()
	h.Write(layerId[:])
	return h.Sum32()
}

func (layerId *LayerId) Bytes() []byte {
	return layerId[:]
}

type Set struct {
	blocks map[BlockId]struct{}
}

type State struct {
	k    uint32          // the iteration number
	ki   uint32          // ?
	s    Set             // the set of blocks
	cert *pb.Certificate // the certificate
}

type ConsensusProcess struct {
	State
	Closer // the consensus is closeable
	layerId     LayerId
	oracle      Rolacle // roles oracle
	signing     Signing
	network     NetworkService
	startTime   time.Time
	inbox       chan *pb.HareMessage
	knowledge   []*pb.HareMessage
	isProcessed map[uint32]bool // TODO: could be empty struct
}

func (proc *ConsensusProcess) NewConsensusProcess(layer LayerId, s Set, oracle Rolacle, signing Signing, p2p NetworkService, broker *Broker) {
	proc.State = State{0, 0, s, nil}
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
		case msg := <-proc.inbox:
			proc.handleMessage(msg) // TODO: should be go handle (?)
		case <-ticker.C:
			proc.nextRound()
		case <-proc.CloseChannel():
			log.Info("Stop event loop, instance aborted")
			return
		}
	}
}

func (proc *ConsensusProcess) handleMessage(hareMsg *pb.HareMessage) {
	// verify iteration
	if hareMsg.Message.K != proc.k {
		return
	}

	// validate signature
	data, err := proto.Marshal(hareMsg.Message)
	if err != nil {
		return
	}
	if !proc.signing.Validate(data, hareMsg.InnerSig) {
		return
	}

	// validate role TODO: in proposal we have additional verification at the end of the round
	if !proc.oracle.ValidateRole(roleFromIteration(hareMsg.Message.K), hareMsg.Message.RoleProof) {
		return
	}

	proc.knowledge = append(proc.knowledge, hareMsg)
}

func (proc *ConsensusProcess) nextRound() {
	log.Info("Next round: %d", proc.k+1)

	// process the round
	go proc.processMessages(proc.knowledge)

	// advance to next round
	proc.k++

	// reset knowledge
	proc.knowledge = make([]*pb.HareMessage, InitialKnowledgeSize)
}

func (proc *ConsensusProcess) processMessages(msg []*pb.HareMessage) {
	// check if process of messages has already been made for this round
	if _, exist := proc.isProcessed[msg[0].Message.K]; exist {
		return
	}

	// TODO: process to create suitable hare message

	m := proc.round0()
	buff, err := proto.Marshal(m)
	if err != nil {
		return
	}

	// send message
	proc.network.Broadcast(ProtoName, buff)

	// mark as processed
	proc.isProcessed[m.Message.K] = true
}

func (proc *ConsensusProcess) round0() *pb.HareMessage {
	// TODO: build SVP msg

	// send proposal
}

func roleFromIteration(k uint32) uint8 {
	if k%4 == 1 { // only round 1 is leader
		return Leader
	}

	return Active
}
