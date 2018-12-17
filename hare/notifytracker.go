package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/pb"
)

type Round4Tracker struct {
	firstNotify *pb.HareMessage
}

func NewRound4Tracker() Round4Tracker {
	r4 := Round4Tracker{}

	return r4
}

func (r4 *Round4Tracker) OnNotify(msg *pb.HareMessage) {
	if r4.firstNotify != nil {
		return
	}

	r4.firstNotify = msg
}

func (r4 *Round4Tracker) GetNotifyMsg() *pb.HareMessage {
	return r4.firstNotify
}


