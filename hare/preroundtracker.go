package hare

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// preRoundTracker tracks pre-round messages.
// The tracker can be queried to check if a value or a set is provable.
// It also provides the ability to filter set from unprovable values.
type preRoundTracker struct {
	preRound  map[string]*Set // maps PubKey->Set of already tracked Values
	tracker   WeightTracker   // keeps track of seen Values
	committee *Committee       // nodes committee
}

func newPreRoundTracker(committee *Committee) *preRoundTracker {
	pre := &preRoundTracker{}
	pre.preRound = make(map[string]*Set, committee.Size)
	pre.tracker = WeightTracker{}
	pre.committee = committee
	return pre
}

// OnPreRound tracks pre-round messages
func (pre *preRoundTracker) OnPreRound(msg *Msg) {
	pub := msg.PubKey.String()
	sToTrack := NewSet(msg.InnerMsg.Values) // assume track all Values
	alreadyTracked := NewDefaultEmptySet()  // assume nothing tracked so far

	if set, exist := pre.preRound[pub]; exist { // not first pre-round msg from this sender
		log.Debug("Duplicate sender %v", pub)
		alreadyTracked = set              // update already tracked Values
		sToTrack.Subtract(alreadyTracked) // subtract the already tracked Values
	}

	// record Values
	for v := range sToTrack.values {
		pre.tracker.Track(v, pre.committee.WeightOf(pub))
	}

	// update the union to include new Values
	pre.preRound[pub] = alreadyTracked.Union(sToTrack)
}

// CanProveValue returns true if the given value is provable, false otherwise.
// a value is said to be provable if it has at least threshold pre-round messages to support it.
func (pre *preRoundTracker) CanProveValue(value types.BlockID) bool {
	// at least threshold occurrences of a given value
	return pre.tracker.WeightOf(value) >= pre.committee.Threshold()
}

// CanProveSet returns true if the give set is provable, false otherwise.
// a set is said to be provable if all his values are provable.
func (pre *preRoundTracker) CanProveSet(set *Set) bool {
	// a set is provable iff all its Values are provable
	for bid := range set.values {
		if !pre.CanProveValue(bid) {
			return false
		}
	}

	return true
}

// FilterSet filters out non-provable values from the given set
func (pre *preRoundTracker) FilterSet(set *Set) {
	for bid := range set.values {
		if !pre.CanProveValue(bid) { // not enough witnesses
			set.Remove(bid)
		}
	}
}
