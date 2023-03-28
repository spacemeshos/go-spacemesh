package hare

import "github.com/spacemeshos/go-spacemesh/common/types"

type Cred struct {
	Count  uint16
	Honest bool
}

type EligibilityTracker struct {
	expectedSize int
	// the eligible identities (string(publicKeyBytes)) for each round and whether it is honest
	nodesByRound map[uint32]map[types.NodeID]*Cred
}

func NewEligibilityTracker(size int) *EligibilityTracker {
	return &EligibilityTracker{
		expectedSize: size,
		nodesByRound: make(map[uint32]map[types.NodeID]*Cred, size),
	}
}

func (et *EligibilityTracker) ForEach(round uint32, apply func(types.NodeID, *Cred)) {
	_, ok := et.nodesByRound[round]
	if !ok {
		return
	}
	for pubKey, crd := range et.nodesByRound[round] {
		apply(pubKey, crd)
	}
}

func (et *EligibilityTracker) Track(nodeID types.NodeID, round uint32, count uint16, honest bool) {
	if _, ok := et.nodesByRound[round]; !ok {
		et.nodesByRound[round] = make(map[types.NodeID]*Cred, et.expectedSize)
	}
	if _, ok := et.nodesByRound[round][nodeID]; !ok {
		et.nodesByRound[round][nodeID] = &Cred{Count: count, Honest: honest}
	} else if !honest { // only update if the identity is newly malicious
		et.nodesByRound[round][nodeID].Honest = false
	}
}
