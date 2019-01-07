package hare

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"sort"
)

type Bytes32 [32]byte
type Signature []byte

type Value struct {
	Bytes32
}
type InstanceId struct {
	Bytes32
}
type MessageType byte

const (
	Status   MessageType = 0
	Proposal MessageType = 1
	Commit   MessageType = 2
	Notify   MessageType = 3
	PreRound MessageType = 10
)

const (
	Round1 = 0
	Round2 = 1
	Round3 = 2
	Round4 = 3
)

func (mType MessageType) String() string {
	switch mType {
	case Status:
		return "Status"
	case Proposal:
		return "Proposal"
	case Commit:
		return "Commit"
	case Notify:
		return "Notify"
	case PreRound:
		return "PreRound"
	default:
		return "Unknown message type"
	}
}

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

// Represents a unique set of values
type Set struct {
	values    map[uint32]Value
	id        uint32
	isIdValid bool
}

// Constructs an empty set
func NewEmptySet(expectedSize int) *Set {
	s := &Set{}
	s.values = make(map[uint32]Value, expectedSize)
	s.id = 0
	s.isIdValid = false

	return s
}

// Constructs a new set from a 2D slice
// Each row represents a single value
func NewSet(data [][]byte) *Set {
	s := &Set{}
	s.isIdValid = false

	s.values = make(map[uint32]Value, len(data))
	for i := 0; i < len(data); i++ {
		bid := Value{NewBytes32(data[i])}
		s.values[bid.Id()] = bid
	}

	return s
}

// Checks if a value is contained in the  set s
func (s *Set) Contains(id Value) bool {
	_, exist := s.values[id.Id()]
	return exist
}

// Adds a value to the set if it doesn't exist already
func (s *Set) Add(id Value) {
	if _, exist := s.values[id.Id()]; exist {
		return
	}

	s.isIdValid = false
	s.values[id.Id()] = id
}

// Removes a value from the set if exist
func (s *Set) Remove(id Value) {
	if _, exist := s.values[id.Id()]; exist {
		return
	}

	s.isIdValid = false
	delete(s.values, id.Id())
}

// Returns true if s and g represents the same set, false otherwise
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

// Returns a representation of the set as 2D slice
// Each row is represents a single value
func (s *Set) To2DSlice() [][]byte {
	slice := make([][]byte, len(s.values))
	i := 0
	for _, v := range s.values {
		slice[i] = make([]byte, len(v.Bytes()))
		copy(slice[i], v.Bytes())
		i++
	}

	return slice
}

func (s *Set) updateId() {
	// order keys
	keys := make([]uint32, len(s.values))
	i := 0
	for k := range s.values {
		keys[i] = k
		i++
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	// calc
	h := fnv.New32()
	for i := 0; i < len(keys); i++ {
		h.Write(s.values[keys[i]].Bytes())
	}

	// update
	s.id = h.Sum32()
	s.isIdValid = true
}

// Returns the id of the set
func (s *Set) Id() uint32 {
	if !s.isIdValid {
		s.updateId()
	}

	return s.id
}

func (s *Set) String() string {
	b := new(bytes.Buffer)
	fmt.Fprintf(b, "Values: \n")
	for _, v := range s.values {
		fmt.Fprintf(b, "%v\r\n", v.Bytes())
	}
	return b.String()
}

// Check if s is a subset of g
func (s *Set) IsSubSetOf(g *Set) bool {
	for _, v := range s.values {
		if !g.Contains(v) {
			return false
		}
	}

	return true
}

// Returns the intersection set of s and g
func (s *Set) Intersection(g *Set) *Set {
	both := NewEmptySet(len(s.values))
	for _, v := range s.values {
		if g.Contains(v) {
			both.Add(v)
		}
	}

	return both
}

// Returns the union set of s and g
func (s *Set) Union(g *Set) *Set {
	union := NewEmptySet(len(s.values) + len(g.values))

	for _, v := range s.values {
		union.Add(v)
	}

	for _, v := range g.values {
		union.Add(v)
	}

	return union
}