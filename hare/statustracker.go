package hare

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/log"
)

// statusTracker tracks status messages.
// Provides functions to build a proposal and validate the statuses.
type statusTracker struct {
	statuses  map[string]*Msg // maps PubKey->StatusMsg
	threshold uint16          // threshold to indicate a set can be proved
	maxKi     int32           // tracks max Ki in tracked status Messages
	maxSet    *Set            // tracks the max raw set in the tracked status Messages
	count     uint16          // the count of valid status messages
	analyzed  bool            // indicates if the Messages have already been analyzed
	log.Log
}

func newStatusTracker(threshold int, expectedSize int) *statusTracker {
	st := &statusTracker{}
	st.statuses = make(map[string]*Msg, expectedSize)
	st.threshold = uint16(threshold)
	st.maxKi = -1 // since Ki>=-1
	st.maxSet = nil
	st.analyzed = false

	return st
}

// RecordStatus records the given status message
func (st *statusTracker) RecordStatus(ctx context.Context, msg *Msg) {
	pub := msg.PubKey
	_, exist := st.statuses[pub.String()]
	if exist { // already handled this sender's status msg
		st.WithContext(ctx).With().Warning("duplicate status message detected",
			log.FieldNamed("sender_id", pub))
		return
	}

	st.statuses[pub.String()] = msg
}

// AnalyzeStatuses analyzes the recorded status messages by the validation function.
func (st *statusTracker) AnalyzeStatuses(isValid func(m *Msg) bool) {
	st.count = 0
	for key, m := range st.statuses {
		if !isValid(m) { // only keep valid Messages
			delete(st.statuses, key)
		} else {
			st.count += m.InnerMsg.EligibilityCount
			if m.InnerMsg.Ki >= st.maxKi { // track max Ki & matching raw set
				st.maxKi = m.InnerMsg.Ki
				st.maxSet = NewSet(m.InnerMsg.Values)
			}
		}
	}

	st.analyzed = true
}

// IsSVPReady returns true if theere are enough statuses to build an SVP, false otherwise.
func (st *statusTracker) IsSVPReady() bool {
	return st.analyzed && st.count >= st.threshold
}

// ProposalSet returns the proposed set if available, nil otherwise.
func (st *statusTracker) ProposalSet(expectedSize int) *Set {
	if st.maxKi == -1 {
		return st.buildUnionSet(expectedSize)
	}

	if st.maxSet == nil { // should be impossible to reach
		panic("maxSet is unexpectedly nil")
	}

	return st.maxSet
}

// returns the union set of all status Messages collected
func (st *statusTracker) buildUnionSet(expectedSize int) *Set {
	unionSet := NewEmptySet(expectedSize)
	for _, m := range st.statuses {
		for bid := range NewSet(m.InnerMsg.Values).values {
			unionSet.Add(bid) // assuming add is unique
		}
	}

	return unionSet
}

// BuildSVP builds the SVP if avilable and returns it, it return false otherwise.
func (st *statusTracker) BuildSVP() *aggregatedMessages {
	if !st.IsSVPReady() {
		return nil
	}

	svp := &aggregatedMessages{}
	for _, m := range st.statuses {
		svp.Messages = append(svp.Messages, m.Message)
	}

	// TODO: set aggregated signature
	if len(svp.Messages) == 0 {
		return nil
	}

	return svp
}
