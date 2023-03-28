package hare

import "github.com/spacemeshos/go-spacemesh/common/types"

type item struct {
	counts map[types.NodeID]struct{}
}

// RefCountTracker tracks the number of references of any object id.
type RefCountTracker struct {
	round        uint32
	eTracker     *EligibilityTracker
	expectedSize int
	table        map[any]*item
}

// NewRefCountTracker creates a new reference count tracker.
func NewRefCountTracker(round uint32, et *EligibilityTracker, expectedSize int) *RefCountTracker {
	return &RefCountTracker{
		round:        round,
		eTracker:     et,
		expectedSize: expectedSize,
		table:        make(map[any]*item, inboxCapacity),
	}
}

// CountStatus returns the number of references to the given id.
func (rt *RefCountTracker) CountStatus(id any) *CountInfo {
	if _, ok := rt.table[id]; !ok {
		return &CountInfo{}
	}

	votes := rt.table[id].counts
	var ci CountInfo
	rt.eTracker.ForEach(rt.round, func(nodeID types.NodeID, cr *Cred) {
		if _, ok := votes[nodeID]; ok {
			if cr.Honest {
				ci.IncHonest(cr.Count)
			} else {
				ci.IncDishonest(cr.Count)
			}
		} else if !cr.Honest {
			ci.IncKnownEquivocator(cr.Count)
		}
	})
	return &ci
}

// Track increases the count for the given object id.
func (rt *RefCountTracker) Track(id any, nodeID types.NodeID) {
	if _, ok := rt.table[id]; !ok {
		rt.table[id] = &item{counts: make(map[types.NodeID]struct{}, rt.expectedSize)}
	}
	rt.table[id].counts[nodeID] = struct{}{}
}
