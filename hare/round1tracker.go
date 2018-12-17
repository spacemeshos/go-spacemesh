package hare

import "github.com/spacemeshos/go-spacemesh/hare/pb"

type Round1Tracker struct {

}

func NewRound1Tracker() Round1Tracker {
	return Round1Tracker{}
}

func (ct *Round1Tracker) OnStatus(msg *pb.HareMessage) {

}

func (ct *Round1Tracker) IsSVPReady() bool {
	return false
}

func (ct *Round1Tracker) GetSVP() *pb.AggregatedMessages {
	svp := &pb.AggregatedMessages{}

	return svp
}
