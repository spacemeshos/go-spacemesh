package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/metrics"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// Tracks pre-round Messages
type PreRoundTracker struct {
	preRound  map[string]*Set  // maps PubKey->Set of already tracked Values
	tracker   *RefCountTracker // keeps track of seen Values
	threshold uint32           // the threshold to prove a single value
}

func NewPreRoundTracker(threshold int, expectedSize int) *PreRoundTracker {
	pre := &PreRoundTracker{}
	pre.preRound = make(map[string]*Set, expectedSize)
	pre.tracker = NewRefCountTracker()
	pre.threshold = uint32(threshold)

	return pre
}

// Tracks a pre-round Message
func (pre *PreRoundTracker) OnPreRound(msg *Msg) {
	pub := signing.NewPublicKey(msg.PubKey)
	sToTrack := NewSet(msg.Message.Values) // assume track all Values
	alreadyTracked := NewSmallEmptySet()   // assume nothing tracked so far

	if set, exist := pre.preRound[pub.String()]; exist { // not first pre-round msg from this sender
		log.Debug("Duplicate sender %v", pub.String())
		alreadyTracked = set              // update already tracked Values
		sToTrack.Subtract(alreadyTracked) // subtract the already tracked Values
	}

	// record Values
	for _, v := range sToTrack.values {
		pre.tracker.Track(v.Id())
		metrics.PreRoundCounter.With("value", v.String()).Add(1)
	}

	// update the union to include new Values
	pre.preRound[pub.String()] = alreadyTracked.Union(sToTrack)
}

// Returns true if the given value is provable, false otherwise
func (pre *PreRoundTracker) CanProveValue(value Value) bool {
	// at least threshold occurrences of a given value
	return pre.tracker.CountStatus(value.Id()) >= pre.threshold
}

// Returns true if the given set is provable, false otherwise
func (pre *PreRoundTracker) CanProveSet(set *Set) bool {
	// a set is provable iff all its Values are provable
	for _, bid := range set.values {
		if !pre.CanProveValue(bid) {
			return false
		}
	}

	return true
}

// Filters out the given set from non-provable Values
func (pre *PreRoundTracker) FilterSet(set *Set) {
	for _, v := range set.values {
		if !pre.CanProveValue(v) { // not enough witnesses
			set.Remove(v)
		}
	}
}
