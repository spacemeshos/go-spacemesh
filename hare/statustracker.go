package hare

import (
	"github.com/spacemeshos/go-spacemesh/log"
)

// StatusTracker tracks status messages.
// Provides functions to build a proposal and validate the statuses.
type StatusTracker struct {
	statuses  map[string]*Msg // maps PubKey->StatusMsg
	threshold int             // threshold to indicate a set can be proved
	maxKi     int32           // tracks max Ki in tracked status Messages
	maxSet    *Set            // tracks the max raw set in the tracked status Messages
	analyzed  bool            // indicates if the Messages have already been analyzed
	log.Log
}

func NewStatusTracker(threshold int, expectedSize int) *StatusTracker {
	st := &StatusTracker{}
	st.statuses = make(map[string]*Msg, expectedSize)
	st.threshold = threshold
	st.maxKi = -1 // since Ki>=-1
	st.maxSet = nil
	st.analyzed = false

	return st
}

// RecordStatus records the given status message
func (st *StatusTracker) RecordStatus(msg *Msg) {
	pub := msg.PubKey
	_, exist := st.statuses[pub.String()]
	if exist { // already handled this sender's status msg
		st.Warning("Duplicated status message detected %v", pub.String())
		return
	}

	st.statuses[pub.String()] = msg
}

// AnalyzeStatuses analyzes the recorded status messages by the validation function.
func (st *StatusTracker) AnalyzeStatuses(isValid func(m *Msg) bool) {
	count := 0
	for key, m := range st.statuses {
		if !isValid(m) || count == st.threshold { // only keep valid Messages
			delete(st.statuses, key)
		} else {
			count++
			if m.InnerMsg.Ki >= st.maxKi { // track max Ki & matching raw set
				st.maxKi = m.InnerMsg.Ki
				st.maxSet = NewSet(m.InnerMsg.Values)
			}
		}
	}

	st.analyzed = true
}

// IsSVPReady returns true if theere are enough statuses to build an SVP, false otherwise.
func (st *StatusTracker) IsSVPReady() bool {
	return st.analyzed && len(st.statuses) == st.threshold
}

// ProposalSet returns the proposed set if available, nil otherwise.
func (st *StatusTracker) ProposalSet(expectedSize int) *Set {
	if st.maxKi == -1 {
		return st.buildUnionSet(expectedSize)
	}

	if st.maxSet == nil { // should be impossible to reach
		panic("maxSet is unexpectedly nil")
	}

	return st.maxSet
}

// returns the union set of all status Messages collected
func (st *StatusTracker) buildUnionSet(expectedSize int) *Set {
	unionSet := NewEmptySet(expectedSize)
	for _, m := range st.statuses {
		for _, bid := range NewSet(m.InnerMsg.Values).values {
			unionSet.Add(bid) // assuming add is unique
		}
	}

	return unionSet
}

// BuildSVP builds the SVP if avilable and returns it, it return false otherwise.
func (st *StatusTracker) BuildSVP() *AggregatedMessages {
	if !st.IsSVPReady() {
		return nil
	}

	svp := &AggregatedMessages{}
	for _, m := range st.statuses {
		svp.Messages = append(svp.Messages, m.Message)
	}

	// TODO: set aggregated signature

	return svp
}
