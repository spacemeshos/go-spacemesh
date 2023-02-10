package hare

type Cred struct {
	Count  uint16
	Honest bool
}

type EligibilityTracker struct {
	expectedSize int
	// the eligible identities (string(publicKeyBytes)) for each round and whether it is honest
	nodesByRound map[uint32]map[string]*Cred
}

func NewEligibilityTracker(size int) *EligibilityTracker {
	return &EligibilityTracker{
		expectedSize: size,
		nodesByRound: make(map[uint32]map[string]*Cred, size),
	}
}

func (et *EligibilityTracker) ForEach(round uint32, apply func(string, *Cred)) {
	_, ok := et.nodesByRound[round]
	if !ok {
		return
	}
	for pubKey, crd := range et.nodesByRound[round] {
		apply(pubKey, crd)
	}
}

func (et *EligibilityTracker) Track(pubKey []byte, round uint32, count uint16, honest bool) {
	if _, ok := et.nodesByRound[round]; !ok {
		et.nodesByRound[round] = make(map[string]*Cred, et.expectedSize)
	}
	if _, ok := et.nodesByRound[round][string(pubKey)]; !ok {
		et.nodesByRound[round][string(pubKey)] = &Cred{Count: count, Honest: honest}
	} else if !honest { // only update if the identity is newly malicious
		et.nodesByRound[round][string(pubKey)].Honest = false
	}
}
