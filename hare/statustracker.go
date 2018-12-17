package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type Round1Tracker struct {
	statuses map[string]*pb.HareMessage
}

func NewRound1Tracker() Round1Tracker {
	return Round1Tracker{}
}

func (r1 *Round1Tracker) RecordStatus(msg *pb.HareMessage) {
	// no need for further processing
	if r1.IsSVPReady() {
		return
	}

	pub, err := crypto.NewPublicKey(msg.PubKey)
	if err != nil {
		log.Warning("Could not construct public key: ", err.Error())
		return
	}

	_, exist := r1.statuses[pub.String()]
	if exist { // already handled this sender's status
		return
	}

	r1.statuses[pub.String()] = msg
}

func (r1 *Round1Tracker) IsSVPReady() bool {
	return len(r1.statuses) >= f+1
}

func (r1 *Round1Tracker) BuildSVP() *pb.AggregatedMessages {
	svp := &pb.AggregatedMessages{}

	return svp
}
