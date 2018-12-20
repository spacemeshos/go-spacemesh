package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type NotifyTracker struct {
	notifies map[string]*pb.HareMessage // tracks PubKey->Notification
	tracker  *RefCountTracker           // tracks ref count to each seen set
}

func NewNotifyTracker(expectedSize int) NotifyTracker {
	nt := NotifyTracker{}
	nt.notifies = make(map[string]*pb.HareMessage, expectedSize)
	nt.tracker = NewRefCountTracker(expectedSize)

	return nt
}

func (nt *NotifyTracker) OnNotify(msg *pb.HareMessage) bool {
	pub, err := crypto.NewPublicKey(msg.PubKey)
	if err != nil {
		log.Warning("Could not construct public key: ", err.Error())
	}

	if _, exist := nt.notifies[pub.String()]; exist { // already seenSenders
		return true
	}

	// keep msg for pub
	nt.notifies[pub.String()] = msg

	// track that set
	s := NewSet(msg.Message.Blocks)
	nt.tracker.Track(s)

	return false
}

func (nt *NotifyTracker) NotificationsCount(s *Set) uint32 {
	return nt.tracker.CountStatus(s)
}
