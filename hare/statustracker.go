package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type StatusTracker struct {
	statuses  map[string]*pb.HareMessage // maps PubKey->StatusMsg
	threshold int                        // threshold to indicate a set can be proved
	maxKi     int32                      // tracks max ki in tracked status messages
	maxRawSet [][]byte                   // tracks the max raw set in the tracked status messages
}

func NewStatusTracker(threshold int, expectedSize int) *StatusTracker {
	st := &StatusTracker{}
	st.statuses = make(map[string]*pb.HareMessage, expectedSize)
	st.threshold = threshold
	st.maxKi = -1 // since ki>=-1
	st.maxRawSet = nil

	return st
}

func (st *StatusTracker) RecordStatus(msg *pb.HareMessage) {
	// no need for further processing
	if st.IsSVPReady() {
		return
	}

	pub, err := crypto.NewPublicKey(msg.PubKey)
	if err != nil {
		log.Warning("Could not construct public key: ", err.Error())
		return
	}

	_, exist := st.statuses[pub.String()]
	if exist { // already handled this sender's status msg
		log.Warning("Duplicated status message detected %v", pub.String())
		return
	}

	// track max ki & matching raw set
	if msg.Message.Ki >= st.maxKi {
		st.maxKi = msg.Message.Ki
		st.maxRawSet = msg.Message.Values
	}

	st.statuses[pub.String()] = msg
}

func (st *StatusTracker) IsSVPReady() bool {
	return len(st.statuses) == st.threshold
}

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
			bid := Value{NewBytes32(buff)}
			unionSet.Add(bid) // assuming add is unique
		}
	}

	return unionSet
}

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
