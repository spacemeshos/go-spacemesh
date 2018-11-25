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
	Status   MessageType = 0
	Proposal MessageType = 1
	Commit   MessageType = 2
	Notify   MessageType = 3
)

func NewBytes32(buff []byte) Bytes32 {
	x := Bytes32{}
	copy(x[:], buff)

	return x
}

func (b32 *Bytes32) Id() uint32 {
	h := fnv.New32()
	h.Write(b32[:])
	return h.Sum32()
}

func (b32 *Bytes32) Bytes() []byte {
	return b32[:]
}

type Set struct {
	blocks []BlockId
}

func NewSet(data [][]byte) *Set {
	s := &Set{}

	s.blocks = make([]BlockId, len(data))
	for i := 0; i < len(data); i++ {
		copy(s.blocks[i].Bytes(), data[i])
	}

	return s
}

func (s *Set) Equals(g *Set) bool {
	if len(s.blocks) != len(g.blocks) {
		return false
	}

	for i :=0;i<len(s.blocks);i++ {
		if s.blocks[i] != g.blocks[i] {
			return false
		}
	}

	return true
}

func (s *Set) To2DSlice() [][]byte {
	slice := make([][]byte, len(s.blocks))
	for i := 0; i < len(s.blocks); i++ {
		copy(slice[i], s.blocks[i].Bytes())
	}

	return slice
}

type AggregatedMessages struct {
	messages []*pb.HareMessage
	aggSig   Signature
}

func NewAggregatedMessages(p *pb.AggregatedMessages) AggregatedMessages {
	m := AggregatedMessages{}

	m.messages = make([]*pb.HareMessage, len(p.Messages))
	for i := 0; i < len(p.Messages); i++ {
		m.messages[i] = p.Messages[i]
	}
	m.aggSig = make([]byte, len(p.AggSig))
	copy(m.aggSig, p.AggSig)

	return m
}

type Certificate struct {
	set *Set
	AggregatedMessages
}

func NewCertificate(p *pb.Certificate) *Certificate {
	c := &Certificate{}

	c.set = NewSet(p.Blocks)
	c.AggregatedMessages = NewAggregatedMessages(p.AggMsgs)

	return c
}

func buildCertificate(s *Set, commits []*pb.HareMessage) *Certificate {
	c := &Certificate{}
	c.set = s
	// TODO...

	return c
}

func (cert *Certificate) Validate(signing Signing) bool {
	// TODO: iterate messages & verify with signing
	return false
}