package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/metrics"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type PreRoundTracker struct {
	preRound  map[string]struct{} // maps PubKey->Pre-Round msg (unique)
	tracker   *RefCountTracker    // keeps track of seen values
	threshold uint32              // the threshold to prove a single value
}

func NewPreRoundTracker(threshold int, expectedSize int) *PreRoundTracker {
	pre := &PreRoundTracker{}
	pre.preRound = make(map[string]struct{}, expectedSize)
	pre.tracker = NewRefCountTracker(expectedSize)
	pre.threshold = uint32(threshold)

	return pre
}

// Tracks a pre-round message
func (pre *PreRoundTracker) OnPreRound(msg *pb.HareMessage) {
	verifier, err := NewVerifier(msg.PubKey)
	if err != nil {
		log.Warning("Could not construct verifier: ", err)
		return
	}

	// only handle first pre-round msg
	if _, exist := pre.preRound[verifier.String()]; exist {
		log.Debug("Duplicate sender %v", verifier.String())
		return
	}

	// record values from msg
	s := NewSet(msg.Message.Values)
	for _, v := range s.values {
		pre.tracker.Track(v)
		metrics.PreRoundCounter.With("value", v.String()).Add(1)
	}

	pre.preRound[verifier.String()] = struct{}{}
}

// Returns true if the given value is provable, false otherwise
func (pre *PreRoundTracker) CanProveValue(value Value) bool {
	// at least threshold occurrences of a given value
	return pre.tracker.CountStatus(value) >= pre.threshold
}

// Returns true if the given set is provable, false otherwise
func (pre *PreRoundTracker) CanProveSet(set *Set) bool {
	// a set is provable iff all its values are provable
	for _, bid := range set.values {
		if !pre.CanProveValue(bid) {
			return false
		}
	}

	return true
}

// Filters the given set according to collected proofs
func (pre *PreRoundTracker) FilterSet(set *Set) {
	for _, v := range set.values {
		if !pre.CanProveValue(v) { // not enough witnesses
			set.Remove(v)
		}
	}
}
