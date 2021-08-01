package hare

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"sort"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

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

// Set represents a unique set of values.
type Set struct {
	valuesMu  sync.RWMutex
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
	s.initWithSize(size)
	s.id = 0
	s.isIDValid = false

	return s
}

// NewSetFromValues creates a set of the provided values.
// Note: duplicated values are ignored.
func NewSetFromValues(values ...types.BlockID) *Set {
	s := &Set{}
	s.initWithSize(len(values))
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

	s.initWithSize(len(data))
	for _, bid := range data {
		s.add(bid)
	}

	return s
}

// Clone creates a copy of the set.
func (s *Set) Clone() *Set {
	clone := NewEmptySet(s.len())
	for _, bid := range s.elements() {
		clone.Add(bid)
	}

	return clone
}

// Contains returns true if the provided value is contained in the set, false otherwise.
func (s *Set) Contains(id types.BlockID) bool {
	return s.contains(id)
}

// Add a value to the set.
// It has no effect if the value already exists in the set.
func (s *Set) Add(id types.BlockID) {
	if s.contains(id) {
		return
	}

	s.isIDValid = false
	s.add(id)
}

// Remove a value from the set.
// It has no effect if the value doesn't exist in the set.
func (s *Set) Remove(id types.BlockID) {
	if s.contains(id) {
		return
	}

	s.isIDValid = false
	s.remove(id)
}

// Equals returns true if the provided set represents this set, false otherwise.
func (s *Set) Equals(g *Set) bool {
	if s.len() != g.len() {
		return false
	}

	for _, bid := range s.elements() {
		if g.contains(bid) {
			return false
		}
	}

	return true
}

// ToSlice returns the array representation of the set.
func (s *Set) ToSlice() []types.BlockID {
	// order keys
	keys := make([]types.BlockID, s.len())
	i := 0
	for _, k := range s.elements() {
		keys[i] = k
		i++
	}
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i].Bytes(), keys[j].Bytes()) == -1 })

	l := make([]types.BlockID, 0, s.len())
	for i := range keys {
		l = append(l, keys[i])
	}
	return l
}

func (s *Set) updateID() {
	// order keys
	keys := make([]types.BlockID, s.len())
	i := 0
	for _, k := range s.elements() {
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
	for _, v := range s.elements() {
		fmt.Fprintf(b, "%v,", v.String())
	}
	if b.Len() >= 1 {
		return b.String()[:b.Len()-1]
	}
	return b.String()
}

// IsSubSetOf returns true if s is a subset of g, false otherwise.
func (s *Set) IsSubSetOf(g *Set) bool {
	for _, v := range s.elements() {
		if !g.Contains(v) {
			return false
		}
	}

	return true
}

// Intersection returns the intersection a new set which represents the intersection of s and g.
func (s *Set) Intersection(g *Set) *Set {
	both := NewEmptySet(s.len())
	for _, v := range s.elements() {
		if g.Contains(v) {
			both.Add(v)
		}
	}

	return both
}

// Union returns a new set which represents the union set of s and g.
func (s *Set) Union(g *Set) *Set {
	union := NewEmptySet(s.len() + g.len())

	for _, v := range s.elements() {
		union.Add(v)
	}

	for _, v := range g.elements() {
		union.Add(v)
	}

	return union
}

// Complement returns a new set that represents the complement of s relatively to the world u.
func (s *Set) Complement(u *Set) *Set {
	comp := NewEmptySet(u.len())
	for _, v := range u.elements() {
		if !s.Contains(v) {
			comp.Add(v)
		}
	}

	return comp
}

// Subtract g from s.
func (s *Set) Subtract(g *Set) {
	for _, v := range g.elements() {
		s.Remove(v)
	}
}

// Size returns the number of elements in the set.
func (s *Set) Size() int {
	return s.len()
}

func (s *Set) init() {
	s.valuesMu.Lock()
	defer s.valuesMu.Unlock()

	s.values = make(map[types.BlockID]struct{})
}

func (s *Set) initWithSize(size int) {
	s.valuesMu.Lock()
	defer s.valuesMu.Unlock()

	s.values = make(map[types.BlockID]struct{}, size)
}

func (s *Set) len() int {
	s.valuesMu.RLock()
	defer s.valuesMu.RUnlock()

	return len(s.values)
}

func (s *Set) contains(id types.BlockID) bool {
	s.valuesMu.RLock()
	defer s.valuesMu.RUnlock()

	_, ok := s.values[id]
	return ok
}

func (s *Set) add(id types.BlockID) {
	s.valuesMu.Lock()
	defer s.valuesMu.Unlock()

	s.values[id] = struct{}{}
}

func (s *Set) remove(id types.BlockID) {
	s.valuesMu.Lock()
	defer s.valuesMu.Unlock()

	delete(s.values, id)
}

func (s *Set) elements() []types.BlockID {
	s.valuesMu.RLock()
	defer s.valuesMu.RUnlock()

	result := make([]types.BlockID, 0, len(s.values))

	for id := range s.values {
		result = append(result, id)
	}

	return result
}
