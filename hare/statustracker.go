package hare

import (
	"github.com/spacemeshos/go-spacemesh/log"
)

// statusTracker tracks status messages.
// Provides functions to build a proposal and validate the statuses.
type statusTracker struct {
	statuses  map[string]*Msg // maps PubKey->StatusMsg
	maxKi     int32           // tracks max Ki in tracked status Messages
	maxSet    *Set            // tracks the max raw set in the tracked status Messages
	ready     bool            // analyzed and meet the requirements
	committee Committee       // nodes Committee
	log.Log
}

func newStatusTracker(committee Committee) *statusTracker {
	st := &statusTracker{}
	st.statuses = make(map[string]*Msg, committee.Size)
	st.committee = committee
	st.maxKi = -1 // since Ki>=-1
	st.maxSet = nil
	return st
}

// RecordStatus records the given status message
func (st *statusTracker) RecordStatus(msg *Msg) {
	pub := msg.PubKey.String()
	_, exist := st.statuses[pub]
	if exist { // already handled this sender's status msg
		st.Warning("Duplicated status message detected %v", pub)
		return
	}

	st.statuses[pub] = msg
}

// AnalyzeStatuses analyzes the recorded status messages by the validation function.
func (st *statusTracker) AnalyzeStatuses(isValid func(m *Msg) bool) {
	statuses := make(map[string]*Msg,len(st.statuses))
	total := uint64(0)
	threshold := st.committee.Threshold()
	for key, m := range st.statuses {
		if isValid(m) {
			w := st.committee.WeightOf(key)
			total += w
			statuses[key] = m
			if m.InnerMsg.Ki >= st.maxKi { // track max Ki & matching raw set
				st.maxKi = m.InnerMsg.Ki
				st.maxSet = NewSet(m.InnerMsg.Values)
			}
			/*
			TODO: need we consider just first messages (in random order) meeting the threshold requirement? Why?
			*/
			if total >= threshold {
				break
			}
		}
	}
	st.ready = total >= threshold
	if st.ready {
		st.statuses = statuses
	}
}

// IsSVPReady returns true if theere are enough statuses to build an SVP, false otherwise.
func (st *statusTracker) IsSVPReady() bool {
	return st.ready
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

	return svp
}
