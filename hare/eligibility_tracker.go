package hare

import (
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type Cred struct {
	Count  uint16
	Honest bool
}

type EligibilityTracker struct {
	expectedSize int
	// the eligible identities (string(publicKeyBytes)) for each round and whether it is honest
	nodesByRound map[uint32]map[types.NodeID]*Cred

	mu sync.RWMutex
}

func NewEligibilityTracker(size int) *EligibilityTracker {
	return &EligibilityTracker{
		expectedSize: size,
		nodesByRound: make(map[uint32]map[types.NodeID]*Cred, size),
	}
}

func (et *EligibilityTracker) ForEach(round uint32, apply func(types.NodeID, *Cred)) {
	et.mu.RLock()
	defer et.mu.RUnlock()
	_, ok := et.nodesByRound[round]
	if !ok {
		return
	}
	for pubKey, crd := range et.nodesByRound[round] {
		apply(pubKey, crd)
	}
}

// Track records a miner's eligibility in a given round and returns if it was known malicious prior to the update.
func (et *EligibilityTracker) Track(nodeID types.NodeID, round uint32, count uint16, honest bool) bool {
	et.mu.Lock()
	defer et.mu.Unlock()
	if _, ok := et.nodesByRound[round]; !ok {
		et.nodesByRound[round] = make(map[types.NodeID]*Cred, et.expectedSize)
	}
	cred, ok := et.nodesByRound[round][nodeID]
	wasDishonest := cred != nil && !cred.Honest
	if !ok {
		et.nodesByRound[round][nodeID] = &Cred{Count: count, Honest: honest}
	} else if !honest { // only update if the identity is newly malicious
		et.nodesByRound[round][nodeID].Honest = false
	}
	return wasDishonest
}

// Dishonest returns whether an eligible identity is known malicious.
func (et *EligibilityTracker) Dishonest(nodeID types.NodeID, round uint32) bool {
	et.mu.RLock()
	defer et.mu.RUnlock()
	if _, ok := et.nodesByRound[round]; ok {
		if cred, ok2 := et.nodesByRound[round][nodeID]; ok2 {
			return !cred.Honest
		}
	}
	return false
}
