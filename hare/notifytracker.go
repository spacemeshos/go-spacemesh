package hare

import (
	"encoding/binary"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/hare/metrics"
	"hash/fnv"
)

// NotifyTracker tracks notify messages.
// It also provides the number of notifications tracked for a given set.
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

// OnNotify tracks the provided notification message.
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

// NotificationsCount returns the number of notifications tracked for the provided set
func (nt *NotifyTracker) NotificationsCount(s *Set) int {
	return int(nt.tracker.CountStatus(s.Id()))
}

// calculates a unique id for the provided k and set.
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

// tracks certificates
func (nt *NotifyTracker) onCertificate(k int32, set *Set) {
	nt.certificates[calcId(k, set)] = struct{}{}
}

// HasCertificate returns true if a certificate exist for the provided set in the provided round, false otherwise.
func (nt *NotifyTracker) HasCertificate(k int32, set *Set) bool {
	_, exist := nt.certificates[calcId(k, set)]
	return exist
}
