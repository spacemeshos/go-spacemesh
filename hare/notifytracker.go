package hare

import (
	"encoding/binary"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/hare/metrics"
	"hash/fnv"
)

// Tracks notify Messages
type NotifyTracker struct {
	notifies     map[string]struct{} // tracks PubKey->Notification
	tracker      *RefCountTracker    // tracks ref count to each seen set
	certificates map[uint32]struct{} // tracks Set->certificate
}

func NewNotifyTracker(expectedSize int) *NotifyTracker {
	nt := &NotifyTracker{}
	nt.notifies = make(map[string]struct{}, expectedSize)
	nt.tracker = NewRefCountTracker()
	nt.certificates = make(map[uint32]struct{}, expectedSize)

	return nt
}

// Track the provided notification InnerMsg
// Returns true if the InnerMsg didn't affect the state, false otherwise
func (nt *NotifyTracker) OnNotify(msg *Msg) bool {
	pub := msg.PubKey
	if _, exist := nt.notifies[pub.String()]; exist { // already seenSenders
		return true // ignored
	}

	// keep msg for pub
	nt.notifies[pub.String()] = struct{}{}

	// track that set
	s := NewSet(msg.InnerMsg.Values)
	nt.onCertificate(msg.InnerMsg.Cert.AggMsgs.Messages[0].InnerMsg.K, s)
	nt.tracker.Track(s.Id())
	metrics.NotifyCounter.With("set_id", fmt.Sprint(s.Id())).Add(1)

	return false
}

// Returns the notification count tracked for the provided set
func (nt *NotifyTracker) NotificationsCount(s *Set) int {
	return int(nt.tracker.CountStatus(s.Id()))
}

func calcId(k int32, set *Set) uint32 {
	hash := fnv.New32()

	// write K
	buff := make([]byte, 4)
	binary.LittleEndian.PutUint32(buff, uint32(k)) // K>=0 because this not pre-round
	hash.Write(buff)

	// TODO: is this hash enough for this usage?
	// write set objectId
	buff = make([]byte, 4)
	binary.LittleEndian.PutUint32(buff, uint32(set.Id()))
	hash.Write(buff)

	return hash.Sum32()
}

func (nt *NotifyTracker) onCertificate(k int32, set *Set) {
	nt.certificates[calcId(k, set)] = struct{}{}
}

// Checks if a certificates exist for the provided set in round K
func (nt *NotifyTracker) HasCertificate(k int32, set *Set) bool {
	_, exist := nt.certificates[calcId(k, set)]
	return exist
}
