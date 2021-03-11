package hare

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"hash/fnv"
	"sort"
)

type instanceID types.LayerID

type messageType byte

// declare all known message types
const (
	status   messageType = 0
	proposal messageType = 1
	commit   messageType = 2
	notify   messageType = 3
	pre      messageType = 10
)

// declare round identifiers
const (
	preRound      = -1
	statusRound   = 0
	proposalRound = 1
	commitRound   = 2
	notifyRound   = 3
)

const defaultSetSize = 200

func (mType messageType) String() string {
	switch mType {
	case status:
		return "Status"
	case proposal:
		return "Proposal"
	case commit:
		return "Commit"
	case notify:
		return "Notify"
	case pre:
		return "PreRound"
	default:
		return "Unknown message type"
	}
}

func (id instanceID) Bytes() []byte {
	idInBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(idInBytes, uint32(id))

	return idInBytes
}

// Set represents a unique set of values.
type Set struct {
	values    map[types.BlockID]struct{}
	id        uint32
	isIDValid bool
}

// NewDefaultEmptySet creates an empty set with the default size.
func NewDefaultEmptySet() *Set {
	return NewEmptySet(defaultSetSize)
}

// NewEmptySet creates an empty set with the provided size.
func NewEmptySet(size int) *Set {
	s := &Set{}
	s.values = make(map[types.BlockID]struct{}, size)
	s.id = 0
	s.isIDValid = false

	return s
}

// NewSetFromValues creates a set of the provided values.
// Note: duplicated values are ignored.
func NewSetFromValues(values ...types.BlockID) *Set {
	s := &Set{}
	s.values = make(map[types.BlockID]struct{}, len(values))
	for _, v := range values {
		s.Add(v)
	}
	s.id = 0
	s.isIDValid = false

	return s
}

// NewSet creates a set from the provided array of values.
// Note: duplicated values are ignored.
func NewSet(data []types.BlockID) *Set {
	s := &Set{}
	s.isIDValid = false

	s.values = make(map[types.BlockID]struct{}, len(data))
	for _, bid := range data {
		s.values[bid] = struct{}{}
	}

	return s
}

// Clone creates a copy of the set.
func (s *Set) Clone() *Set {
	clone := NewEmptySet(len(s.values))
	for bid := range s.values {
		clone.Add(bid)
	}

	return clone
}

// Contains returns true if the provided value is contained in the set, false otherwise.
func (s *Set) Contains(id types.BlockID) bool {
	_, exist := s.values[id]
	return exist
}

// Add a value to the set.
// It has no effect if the value already exists in the set.
func (s *Set) Add(id types.BlockID) {
	if _, exist := s.values[id]; exist {
		return
	}

	s.isIDValid = false
	s.values[id] = struct{}{}
}

// Remove a value from the set.
// It has no effect if the value doesn't exist in the set.
func (s *Set) Remove(id types.BlockID) {
	if _, exist := s.values[id]; !exist {
		return
	}

	s.isIDValid = false
	delete(s.values, id)
}

// Equals returns true if the provided set represents this set, false otherwise.
func (s *Set) Equals(g *Set) bool {
	if len(s.values) != len(g.values) {
		return false
	}

	for bid := range s.values {
		if _, exist := g.values[bid]; !exist {
			return false
		}
	}

	return true
}

// ToSlice returns the array representation of the set.
func (s *Set) ToSlice() []types.BlockID {
	// order keys
	keys := make([]types.BlockID, len(s.values))
	i := 0
	for k := range s.values {
		keys[i] = k
		i++
	}
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i].Bytes(), keys[j].Bytes()) == -1 })

	l := make([]types.BlockID, 0, len(s.values))
	for i := range keys {
		l = append(l, keys[i])
	}
	return l
}

func (s *Set) updateID() {
	// order keys
	keys := make([]types.BlockID, len(s.values))
	i := 0
	for k := range s.values {
		keys[i] = k
		i++
	}
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i].Bytes(), keys[j].Bytes()) == -1 })

	// calc
	h := fnv.New32()
	for i := 0; i < len(keys); i++ {
		h.Write(keys[i].Bytes())
	}

	// update
	s.id = h.Sum32()
	s.isIDValid = true
}

// ID returns the ObjectID of the set.
func (s *Set) ID() uint32 {
	if !s.isIDValid {
		s.updateID()
	}

	return s.id
}

func (s *Set) String() string {
	// TODO: should improve
	b := new(bytes.Buffer)
	for v := range s.values {
		fmt.Fprintf(b, "%v,", v.String())
	}
	if b.Len() >= 1 {
		return b.String()[:b.Len()-1]
	}
	return b.String()
}

// IsSubSetOf returns true if s is a subset of g, false otherwise.
func (s *Set) IsSubSetOf(g *Set) bool {
	for v := range s.values {
		if !g.Contains(v) {
			return false
		}
	}

	return true
}

// Intersection returns the intersection a new set which represents the intersection of s and g.
func (s *Set) Intersection(g *Set) *Set {
	both := NewEmptySet(len(s.values))
	for v := range s.values {
		if g.Contains(v) {
			both.Add(v)
		}
	}

	return both
}

// Union returns a new set which represents the union set of s and g.
func (s *Set) Union(g *Set) *Set {
	union := NewEmptySet(len(s.values) + len(g.values))

	for v := range s.values {
		union.Add(v)
	}

	for v := range g.values {
		union.Add(v)
	}

	return union
}

// Complement returns a new set that represents the complement of s relatively to the world u.
func (s *Set) Complement(u *Set) *Set {
	comp := NewEmptySet(len(u.values))
	for v := range u.values {
		if !s.Contains(v) {
			comp.Add(v)
		}
	}

	return comp
}

// Subtract g from s.
func (s *Set) Subtract(g *Set) {
	for v := range g.values {
		s.Remove(v)
	}
}

// Size returns the number of elements in the set.
func (s *Set) Size() int {
	return len(s.values)
}
