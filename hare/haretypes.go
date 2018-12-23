package hare

import (
	"hash/fnv"
	"sort"
)

type Bytes32 [32]byte
type Signature []byte

type Value struct {
	Bytes32
}
type SetId struct {
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
	values map[uint32]Value
}

func NewEmptySet(expectedSize int) *Set {
	s := &Set{}
	s.values = make(map[uint32]Value, expectedSize)

	return s
}

func NewSet(data [][]byte) *Set {
	s := &Set{}

	s.values = make(map[uint32]Value, len(data))
	for i := 0; i < len(data); i++ {
		bid := Value{NewBytes32(data[i])}
		s.values[bid.Id()] = bid
	}

	return s
}

func (s *Set) Contains(id Value) bool {
	_, exist := s.values[id.Id()]
	return exist
}

func (s *Set) Add(id Value) {
	if _, exist := s.values[id.Id()]; exist {
		return
	}

	s.values[id.Id()] = id
}

func (s *Set) Remove(id Value) {
	delete(s.values, id.Id())
}

func (s *Set) Equals(g *Set) bool {
	if len(s.values) != len(g.values) {
		return false
	}

	for _, bid := range s.values {
		if _, exist := g.values[bid.Id()]; !exist {
			return false
		}
	}

	return true
}

func (s *Set) To2DSlice() [][]byte {
	slice := make([][]byte, len(s.values))
	i := 0
	for _, v := range s.values {
		slice[i] = make([]byte, 32)
		copy(slice[i], v.Bytes())
		i++
	}

	return slice
}

func (s *Set) Id() uint32 {
	h := fnv.New32()

	keys := make([]uint32, len(s.values))
	i := 0
	for k := range s.values {
		keys[i] = k
		i++
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	for i := 0; i < len(keys); i++ {
		h.Write(s.values[keys[i]].Bytes())
	}

	return h.Sum32()
}
