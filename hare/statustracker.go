package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

// Tracks status messages
type StatusTracker struct {
	statuses  map[string]*pb.HareMessage // maps PubKey->StatusMsg
	threshold int                        // threshold to indicate a set can be proved
	maxKi     int32                      // tracks max ki in tracked status messages
	maxRawSet []uint64                   // tracks the max raw set in the tracked status messages
	analyzed  bool                       // indicates if the messages have already been analyzed
	log.Log
}

func NewStatusTracker(threshold int, expectedSize int) *StatusTracker {
	st := &StatusTracker{}
	st.statuses = make(map[string]*pb.HareMessage, expectedSize)
	st.threshold = threshold
	st.maxKi = -1 // since ki>=-1
	st.maxRawSet = nil
	st.analyzed = false

	return st
}

// Records the given status message
func (st *StatusTracker) RecordStatus(msg *pb.HareMessage) {
	verifier, err := NewVerifier(msg.PubKey)
	if err != nil {
		st.Warning("Could not construct verifier: ", err)
		return
	}

	_, exist := st.statuses[verifier.String()]
	if exist { // already handled this sender's status msg
		st.Warning("Duplicated status message detected %v", verifier.String())
		return
	}

	st.statuses[verifier.String()] = msg
}

// Analyzes the recorded status messages according to the validation function
func (st *StatusTracker) AnalyzeStatuses(isValid func(m *pb.HareMessage) bool) {
	count := 0
	for key, m := range st.statuses {
		if !isValid(m) || count == st.threshold { // only keep valid messages
			delete(st.statuses, key)
		} else {
			count++
			if m.Message.Ki >= st.maxKi { // track max ki & matching raw set
				st.maxKi = m.Message.Ki
				st.maxRawSet = m.Message.Values
			}
		}
	}

	st.analyzed = true
}

// Checks if the SVP is ready
func (st *StatusTracker) IsSVPReady() bool {
	return st.analyzed && len(st.statuses) == st.threshold
}

// Returns the proposed set if available, nil otherwise
func (st *StatusTracker) ProposalSet(expectedSize int) *Set {
	if st.maxKi == -1 {
		return st.buildUnionSet(expectedSize)
	}

	if st.maxRawSet == nil { // should be impossible to reach
		panic("maxRawSet is unexpectedly nil")
	}

	return NewSet(st.maxRawSet)
}

// Returns the union set of all status messages collected
func (st *StatusTracker) buildUnionSet(expectedSize int) *Set {
	unionSet := NewEmptySet(expectedSize)
	for _, m := range st.statuses {
		for _, buff := range m.Message.Values {
			bid := Value{mesh.BlockID(buff)}
			unionSet.Add(bid) // assuming add is unique
		}
	}

	return unionSet
}

// Builds the SVP
// Returns the SVP if available, nil otherwise
func (st *StatusTracker) BuildSVP() *pb.AggregatedMessages {
	if !st.IsSVPReady() {
		return nil
	}

	svp := &pb.AggregatedMessages{}
	for _, m := range st.statuses {
		svp.Messages = append(svp.Messages, m)
	}

	// TODO: set aggregated signature

	return svp
}
