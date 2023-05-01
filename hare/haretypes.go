package hare

import (
	"bytes"
	"sort"
	"strings"
	"sync"

	"golang.org/x/exp/maps"

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
		return "status"
	case proposal:
		return "proposal"
	case commit:
		return "commit"
	case notify:
		return "notify"
	case pre:
		return "preround"
	default:
		return "Unknown message type"
	}
}

// Set represents a unique set of values.
type Set struct {
	mu     sync.RWMutex
	values map[types.ProposalID]struct{}
	sid    types.Hash32
}

// NewDefaultEmptySet creates an empty set with the default size.
func NewDefaultEmptySet() *Set {
	return NewEmptySet(defaultSetSize)
}

// NewEmptySet creates an empty set with the provided size.
func NewEmptySet(size int) *Set {
	return &Set{values: make(map[types.ProposalID]struct{}, size)}
}

// NewSetFromValues creates a set of the provided values.
// Note: duplicated values are ignored.
func NewSetFromValues(values ...types.ProposalID) *Set {
	s := &Set{values: make(map[types.ProposalID]struct{}, len(values))}
	for _, v := range values {
		s.values[v] = struct{}{}
	}
	return s
}

// NewSet creates a set from the provided array of values.
// Note: duplicated values are ignored.
func NewSet(data []types.ProposalID) *Set {
	s := &Set{values: make(map[types.ProposalID]struct{}, len(data))}
	for _, v := range data {
		s.values[v] = struct{}{}
	}
	return s
}

// Clone creates a copy of the set.
func (s *Set) Clone() *Set {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clone := NewEmptySet(len(s.values))
	for v := range s.values {
		clone.values[v] = struct{}{}
	}

	return clone
}

// Contains returns true if the provided value is contained in the set, false otherwise.
func (s *Set) Contains(id types.ProposalID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, ok := s.values[id]
	return ok
}

// Add a value to the set.
// It has no effect if the value already exists in the set.
func (s *Set) Add(id types.ProposalID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exist := s.values[id]; exist {
		return
	}

	s.sid = types.Hash32{}
	s.values[id] = struct{}{}
}

// Remove a value from the set.
// It has no effect if the value doesn't exist in the set.
func (s *Set) Remove(id types.ProposalID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exist := s.values[id]; !exist {
		return
	}

	s.sid = types.Hash32{}
	delete(s.values, id)
}

// Equals returns true if the provided set represents this set, false otherwise.
func (s *Set) Equals(g *Set) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(s.values) != len(g.values) {
		return false
	}

	for v := range s.values {
		if _, exist := g.values[v]; !exist {
			return false
		}
	}

	return true
}

// ToSlice returns the array representation of the set.
func (s *Set) ToSlice() []types.ProposalID {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// order keys
	return s.sortedLocked()
}

// ID returns the ObjectID of the set.
func (s *Set) ID() types.Hash32 {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sid != (types.Hash32{}) {
		return s.sid
	}

	// order keys
	keys := s.sortedLocked()

	// calc
	h := hash.New()
	for i := 0; i < len(keys); i++ {
		h.Write(keys[i].Bytes())
	}

	// update
	s.sid = types.BytesToHash(h.Sum([]byte{}))
	return s.sid
}

func (s *Set) sortedLocked() []types.ProposalID {
	result := maps.Keys(s.values)
	sort.Slice(result, func(i, j int) bool { return bytes.Compare(result[i].Bytes(), result[j].Bytes()) == -1 })

	return result
}

func (s *Set) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var sb strings.Builder
	idx := len(s.values)
	for v := range s.values {
		idx--
		sb.WriteString(v.String())
		if idx > 0 {
			sb.WriteString(",")
		}
	}
	return sb.String()
}

// IsSubSetOf returns true if s is a subset of g, false otherwise.
func (s *Set) IsSubSetOf(g *Set) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for v := range s.values {
		if !g.Contains(v) {
			return false
		}
	}

	return true
}

// Intersection returns the intersection a new set which represents the intersection of s and g.
func (s *Set) Intersection(g *Set) *Set {
	s.mu.RLock()
	defer s.mu.RUnlock()

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
	s.mu.RLock()
	defer s.mu.RUnlock()

	g.mu.RLock()
	defer g.mu.RUnlock()

	union := NewEmptySet(len(s.values) + len(g.values))

	for v := range s.values {
		union.values[v] = struct{}{}
	}

	for v := range g.values {
		union.values[v] = struct{}{}
	}

	return union
}

// Complement returns a new set that represents the complement of s relatively to the world u.
func (s *Set) Complement(u *Set) *Set {
	u.mu.RLock()
	defer u.mu.RUnlock()

	comp := NewEmptySet(len(u.values))
	for v := range u.values {
		if !s.Contains(v) {
			comp.values[v] = struct{}{}
		}
	}

	return comp
}

// Subtract g from s.
func (s *Set) Subtract(g *Set) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	for v := range g.values {
		s.Remove(v)
	}
}

// Size returns the number of SortedElements in the set.
func (s *Set) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.values)
}
