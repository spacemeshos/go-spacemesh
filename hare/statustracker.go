package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type StatusTracker struct {
	statuses  map[string]*pb.HareMessage
	threshold int
}

func NewStatusTracker(threshold int) StatusTracker {
	st := StatusTracker{}
	st.statuses = make(map[string]*pb.HareMessage, N)
	st.threshold = threshold

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
	if exist { // already handled this sender's status
		return
	}

	st.statuses[pub.String()] = msg
}

func (st *StatusTracker) IsSVPReady() bool {
	return len(st.statuses) == st.threshold
}

func (st *StatusTracker) BuildUnionSet() *Set {
	unionSet := NewEmptySet()
	for _, m := range st.statuses {
		for _, buff := range m.Message.Blocks {
			bid := BlockId{NewBytes32(buff)}
			if !unionSet.Contains(bid) {
				unionSet.Add(bid)
			}
		}
	}

	return unionSet
}

func (st *StatusTracker) BuildSVP() *pb.AggregatedMessages {
	svp := &pb.AggregatedMessages{}

	for _, m := range st.statuses {
		svp.Messages = append(svp.Messages, m)
	}

	// TODO: set aggregated signature

	return svp
}
