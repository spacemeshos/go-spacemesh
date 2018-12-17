package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/pb"
)

type NotifyTracker struct {
	firstNotify *pb.HareMessage
}

func NewNotifyTracker() NotifyTracker {
	r4 := NotifyTracker{}

	return r4
}

func (nt *NotifyTracker) OnNotify(msg *pb.HareMessage) {
	if nt.firstNotify != nil {
		return
	}

	nt.firstNotify = msg
}

func (nt *NotifyTracker) GetNotifyMsg() *pb.HareMessage {
	return nt.firstNotify
}


