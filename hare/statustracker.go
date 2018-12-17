package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type StatusTracker struct {
	statuses map[string]*pb.HareMessage
}

func NewStatusTracker() StatusTracker {
	return StatusTracker{}
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
	return len(st.statuses) >= f+1
}

func (st *StatusTracker) BuildSVP() *pb.AggregatedMessages {
	svp := &pb.AggregatedMessages{}

	return svp
}
