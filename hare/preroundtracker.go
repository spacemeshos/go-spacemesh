package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/metrics"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type PreRoundTracker struct {
	preRound  map[string]*Set  // maps PubKey->Pre-Round msg (unique)
	tracker   *RefCountTracker // keeps track of seen values
	threshold uint32           // the threshold to prove a single value
}

func NewPreRoundTracker(threshold int, expectedSize int) *PreRoundTracker {
	pre := &PreRoundTracker{}
	pre.preRound = make(map[string]*Set, expectedSize)
	pre.tracker = NewRefCountTracker()
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

	var sToTrack *Set = nil
	if unionSet, exist := pre.preRound[verifier.String()]; exist { // not first pre-round msg from this sender
		log.Debug("Duplicate sender %v", verifier.String())
		claimedSet := NewSet(msg.Message.Values)                     // new claimed set
		sToTrack = claimedSet                                        // start with the claimed set
		sToTrack.Subtract(unionSet)                                  // subtract the union of all sets we have seen so far from this sender
		pre.preRound[verifier.String()] = unionSet.Union(claimedSet) // update the union to include new values
	} else { // first pre-round
		sToTrack = NewSet(msg.Message.Values)      // first pre-round, track all values
		pre.preRound[verifier.String()] = sToTrack // the union is the set we received
	}

	// record values
	for _, v := range sToTrack.values {
		pre.tracker.Track(v.Id())
		metrics.PreRoundCounter.With("value", v.String()).Add(1)
	}

}

// Returns true if the given value is provable, false otherwise
func (pre *PreRoundTracker) CanProveValue(value Value) bool {
	// at least threshold occurrences of a given value
	return pre.tracker.CountStatus(value.Id()) >= pre.threshold
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
