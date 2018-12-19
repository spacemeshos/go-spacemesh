package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type NotifyTracker struct {
	notifies map[string]*pb.HareMessage
	tracker *RefCountTracker
}

func NewNotifyTracker(size uint32) NotifyTracker {
	nt := NotifyTracker{}
	nt.notifies = make(map[string]*pb.HareMessage, size)
	nt.tracker = NewRefCountTracker(size)

	return nt
}

func (nt *NotifyTracker) OnNotify(msg *pb.HareMessage) bool {
	pub, err := crypto.NewPublicKey(msg.PubKey)
	if err != nil {
		log.Warning("Could not construct public key: ", err.Error())
	}

	if _, exist := nt.notifies[pub.String()]; exist { // already exist
		return true
	}

	nt.notifies[pub.String()] = msg

	s := NewSet(msg.Message.Blocks)
	nt.tracker.Track(s)

	return false
}

func (nt *NotifyTracker) NotificationsCount(s *Set) uint32 {
	return nt.tracker.CountStatus(s)
}



