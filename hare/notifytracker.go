package hare

import (
	"encoding/binary"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"hash/fnv"
)

type NotifyTracker struct {
	notifies     map[string]struct{} // tracks PubKey->Notification
	tracker      *RefCountTracker           // tracks ref count to each seen set
	certificates map[uint32]struct{}        // tracks Set->certificate
}

func NewNotifyTracker(expectedSize int) *NotifyTracker {
	nt := &NotifyTracker{}
	nt.notifies = make(map[string]struct{}, expectedSize)
	nt.tracker = NewRefCountTracker(expectedSize)
	nt.certificates = make(map[uint32]struct{}, expectedSize)

	return nt
}

// update state on notification message
// It returns true if we ignored this message and false otherwise
func (nt *NotifyTracker) OnNotify(msg *pb.HareMessage) bool {
	verifier, err := NewVerifier(msg.PubKey)
	if err != nil {
		log.Warning("Could not construct verifier: ", err)
		return true
	}

	if _, exist := nt.notifies[verifier.String()]; exist { // already seenSenders
		return true // ignored
	}

	// keep msg for pub
	nt.notifies[verifier.String()] = struct{}{}

	// track that set
	s := NewSet(msg.Message.Values)
	nt.onCertificate(msg.Cert.AggMsgs.Messages[0].Message.K, s)
	nt.tracker.Track(s)

	return false
}

func (nt *NotifyTracker) NotificationsCount(s *Set) int {
	return int(nt.tracker.CountStatus(s))
}

func calcId(k uint32, set *Set) uint32 {
	hash := fnv.New32()

	// write k
	buff := make([]byte, 4)
	binary.LittleEndian.PutUint32(buff, k)
	hash.Write(buff)

	// write set id
	buff = make([]byte, 4)
	binary.LittleEndian.PutUint32(buff, set.Id())
	hash.Write(buff)

	return hash.Sum32()
}

func (nt *NotifyTracker) onCertificate(k uint32, set *Set) {
	nt.certificates[calcId(k, set)] = struct{}{}
}

func (nt *NotifyTracker) HasCertificate(k int32, set *Set) bool {
	_, exist := nt.certificates[calcId(uint32(k), set)]
	return exist
}
