package hare

import (
	"bytes"
	"fmt"
	"sort"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/hash"
)

// MessageType is a message type.
type MessageType byte

// declare all known message types.
const (
	status   MessageType = 0
	proposal MessageType = 1
	commit   MessageType = 2
	notify   MessageType = 3
	pre      MessageType = 10
)

const (
	preRound      = eligibility.HarePreRound
	statusRound   = eligibility.HareStatusRound
	proposalRound = eligibility.HareProposalRound
	commitRound   = eligibility.HareCommitRound
	notifyRound   = eligibility.HareNotifyRound
)

const defaultSetSize = 200

func (mType MessageType) String() string {
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
	values    map[types.ProposalID]struct{}
	id        types.Hash32
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
	s.id = types.Hash32{}
	s.isIDValid = false

	return s
}

// NewSetFromValues creates a set of the provided values.
// Note: duplicated values are ignored.
func NewSetFromValues(values ...types.ProposalID) *Set {
	s := &Set{}
	s.initWithSize(len(values))
	for _, v := range values {
		s.Add(v)
	}
	s.id = types.Hash32{}
	s.isIDValid = false

	return s
}

// NewSet creates a set from the provided array of values.
// Note: duplicated values are ignored.
func NewSet(data []types.ProposalID) *Set {
	s := &Set{}
	s.isIDValid = false

	s.initWithSize(len(data))

	// SAFETY: It's safe not to lock here as `s` was just
	// created and nobody else has a access to it yet.
	for _, bid := range data {
		s.values[bid] = struct{}{}
	}

	return s
}

// Clone creates a copy of the set.
func (s *Set) Clone() *Set {
	s.valuesMu.RLock()
	defer s.valuesMu.RUnlock()

	clone := NewEmptySet(len(s.values))
	for bid := range s.values {
		clone.Add(bid)
	}

	return clone
}

// Contains returns true if the provided value is contained in the set, false otherwise.
func (s *Set) Contains(id types.ProposalID) bool {
	return s.contains(id)
}

// Add a value to the set.
// It has no effect if the value already exists in the set.
func (s *Set) Add(id types.ProposalID) {
	s.valuesMu.Lock()
	defer s.valuesMu.Unlock()

	if _, exist := s.values[id]; exist {
		return
	}

	s.isIDValid = false
	s.values[id] = struct{}{}
}

// Remove a value from the set.
// It has no effect if the value doesn't exist in the set.
func (s *Set) Remove(id types.ProposalID) {
	s.valuesMu.Lock()
	defer s.valuesMu.Unlock()

	if _, exist := s.values[id]; !exist {
		return
	}

	s.isIDValid = false
	delete(s.values, id)
}

// Equals returns true if the provided set represents this set, false otherwise.
func (s *Set) Equals(g *Set) bool {
	s.valuesMu.RLock()
	defer s.valuesMu.RUnlock()

	g.valuesMu.RLock()
	defer g.valuesMu.RUnlock()

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
func (s *Set) ToSlice() []types.ProposalID {
	s.valuesMu.RLock()
	defer s.valuesMu.RUnlock()

	// order keys
	keys := make([]types.ProposalID, len(s.values))
	i := 0
	for k := range s.values {
		keys[i] = k
		i++
	}
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i].Bytes(), keys[j].Bytes()) == -1 })

	l := make([]types.ProposalID, 0, len(s.values))
	for i := range keys {
		l = append(l, keys[i])
	}
	return l
}

func (s *Set) updateID() {
	s.valuesMu.RLock()
	defer s.valuesMu.RUnlock()

	// order keys
	keys := make([]types.ProposalID, len(s.values))
	i := 0
	for k := range s.values {
		keys[i] = k
		i++
	}
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i].Bytes(), keys[j].Bytes()) == -1 })

	// calc
	h := hash.New()
	for i := 0; i < len(keys); i++ {
		h.Write(keys[i].Bytes())
	}

	// update
	s.id = types.BytesToHash(h.Sum([]byte{}))
	s.isIDValid = true
}

// ID returns the ObjectID of the set.
func (s *Set) ID() types.Hash32 {
	if !s.isIDValid {
		s.updateID()
	}

	return s.id
}

func (s *Set) String() string {
	s.valuesMu.RLock()
	defer s.valuesMu.RUnlock()

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
	s.valuesMu.RLock()
	defer s.valuesMu.RUnlock()

	for v := range s.values {
		if !g.Contains(v) {
			return false
		}
	}

	return true
}

// Intersection returns the intersection a new set which represents the intersection of s and g.
func (s *Set) Intersection(g *Set) *Set {
	s.valuesMu.RLock()
	defer s.valuesMu.RUnlock()

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
	s.valuesMu.RLock()
	defer s.valuesMu.RUnlock()

	g.valuesMu.RLock()
	defer g.valuesMu.RUnlock()

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
	s.valuesMu.RLock()
	defer s.valuesMu.RUnlock()

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
	g.valuesMu.RLock()
	defer g.valuesMu.RUnlock()

	for v := range g.values {
		s.Remove(v)
	}
}

// Size returns the number of elements in the set.
func (s *Set) Size() int {
	return s.len()
}

func (s *Set) initWithSize(size int) {
	s.valuesMu.Lock()
	defer s.valuesMu.Unlock()

	s.values = make(map[types.ProposalID]struct{}, size)
}

func (s *Set) len() int {
	s.valuesMu.RLock()
	defer s.valuesMu.RUnlock()

	return len(s.values)
}

func (s *Set) contains(id types.ProposalID) bool {
	s.valuesMu.RLock()
	defer s.valuesMu.RUnlock()

	_, ok := s.values[id]
	return ok
}

func (s *Set) elements() []types.ProposalID {
	s.valuesMu.RLock()
	defer s.valuesMu.RUnlock()

	result := make([]types.ProposalID, 0, len(s.values))

	for id := range s.values {
		result = append(result, id)
	}

	return result
}
