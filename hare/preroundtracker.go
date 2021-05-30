package hare

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// preRoundTracker tracks pre-round messages.
// The tracker can be queried to check if a value or a set is provable.
// It also provides the ability to filter set from unprovable values.
type preRoundTracker struct {
	preRound  map[string]*Set  // maps PubKey->Set of already tracked Values
	tracker   *RefCountTracker // keeps track of seen Values
	threshold uint32           // the threshold to prove a value
	logger    log.Log
}

func newPreRoundTracker(threshold int, expectedSize int, logger log.Log) *preRoundTracker {
	pre := &preRoundTracker{}
	pre.preRound = make(map[string]*Set, expectedSize)
	pre.tracker = NewRefCountTracker()
	pre.threshold = uint32(threshold)
	pre.logger = logger

	return pre
}

// OnPreRound tracks pre-round messages
func (pre *preRoundTracker) OnPreRound(ctx context.Context, msg *Msg) {
	logger := pre.logger.WithContext(ctx)

	pub := msg.PubKey
	logger.With().Debug("received preround message",
		log.String("sender_id", pub.ShortString()),
		log.Int("num_values", len(msg.InnerMsg.Values)))
	eligibilityCount := uint32(msg.InnerMsg.EligibilityCount)
	sToTrack := NewSet(msg.InnerMsg.Values) // assume track all Values
	alreadyTracked := NewDefaultEmptySet()  // assume nothing tracked so far

	if set, exist := pre.preRound[pub.String()]; exist { // not first pre-round msg from this sender
		logger.With().Debug("duplicate preround msg sender", log.String("sender_id", pub.ShortString()))
		alreadyTracked = set              // update already tracked Values
		sToTrack.Subtract(alreadyTracked) // subtract the already tracked Values
	}

	// record Values
	for v := range sToTrack.values {
		pre.tracker.Track(v, eligibilityCount)
	}

	// update the union to include new Values
	pre.preRound[pub.String()] = alreadyTracked.Union(sToTrack)
}

// CanProveValue returns true if the given value is provable, false otherwise.
// a value is said to be provable if it has at least threshold pre-round votes to support it.
func (pre *preRoundTracker) CanProveValue(value types.BlockID) bool {
	// at least threshold occurrences of a given value
	countStatus := pre.tracker.CountStatus(value)
	pre.logger.With().Debug("preround tracker count for blockid",
		value,
		log.Uint32("count", countStatus),
		log.Uint32("threshold", pre.threshold))
	return countStatus >= pre.threshold
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
