package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"hash/fnv"
)

type Bytes32 [32]byte
type Signature []byte

type BlockId struct {
	Bytes32
}
type LayerId struct {
	Bytes32
}
type MessageType byte

const (
	PreRound MessageType = 0
	Status   MessageType = 1
	Proposal MessageType = 2
	Commit   MessageType = 3
	Notify   MessageType = 4
)

func NewBytes32(buff []byte) Bytes32 {
	x := Bytes32{}
	copy(x[:], buff)

	return x
}

func (b32 Bytes32) Id() uint32 {
	h := fnv.New32()
	h.Write(b32[:])
	return h.Sum32()
}

func (b32 Bytes32) Bytes() []byte {
	return b32[:]
}

type Set struct {
	blocks map[uint32]BlockId
}

func NewEmptySet() *Set {
	s := &Set{}
	s.blocks = make(map[uint32]BlockId, layerSize)

	return s
}

func NewSet(data [][]byte) *Set {
	s := &Set{}

	s.blocks = make(map[uint32]BlockId, len(data))
	for i := 0; i < len(data); i++ {
		bid := BlockId{NewBytes32(data[i])}
		s.blocks[bid.Id()] = bid
	}

	return s
}

func (s *Set) Add(id BlockId) {
	if _, exist := s.blocks[id.Id()]; exist {
		return
	}

	s.blocks[id.Id()] = id
}

func (s *Set) Remove(id BlockId) {
	delete(s.blocks, id.Id())
}

func (s *Set) Equals(g *Set) bool {
	if len(s.blocks) != len(g.blocks) {
		return false
	}

	for _, bid := range s.blocks {
		if _, exist := g.blocks[bid.Id()]; !exist {
			return false
		}
	}

	return true
}

func (s *Set) To2DSlice() [][]byte {
	slice := make([][]byte, len(s.blocks))
	i := 0
	for _, v := range s.blocks {
		slice[i] = make([]byte, 32)
		copy(slice[i], v.Bytes())
		i++
	}

	return slice
}

func (s *Set) Id() uint32 {
	h := fnv.New32()

	for _, v := range s.blocks {
		h.Write(v.Bytes())
	}

	return h.Sum32()
}

type AggregatedMessages struct {
	messages []*pb.HareMessage
	aggSig   Signature
}

func NewAggregatedMessages(msgs []*pb.HareMessage, aggSig Signature) *AggregatedMessages {
	m := &AggregatedMessages{}
	m.messages = msgs
	m.aggSig = aggSig

	return m
}

func AggregatedMessagesFromProto(p *pb.AggregatedMessages) *AggregatedMessages {
	m := &AggregatedMessages{}

	m.messages = make([]*pb.HareMessage, len(p.Messages))
	for i := 0; i < len(p.Messages); i++ {
		m.messages[i] = p.Messages[i]
	}
	m.aggSig = make([]byte, len(p.AggSig))
	copy(m.aggSig, p.AggSig)

	return m
}

func (agg *AggregatedMessages) ToProto() *pb.AggregatedMessages {
	m := &pb.AggregatedMessages{}
	m.Messages = agg.messages
	m.AggSig = agg.aggSig

	return m
}

func (agg *AggregatedMessages) Validate() bool {
	return true
}
