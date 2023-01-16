package hare

import (
	"context"
	"encoding/binary"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
)

// notifyTracker tracks notify messages.
// It also provides the number of notifications tracked for a given set.
type notifyTracker struct {
	logger       log.Log
	malCh        chan types.MalfeasanceGossip
	notifies     map[string]*types.HareProofMsg // tracks PubKey->Notification
	tracker      *RefCountTracker               // tracks ref count to each seen set
	certificates map[types.Hash32]struct{}      // tracks Set->certificate
}

func newNotifyTracker(logger log.Log, mch chan types.MalfeasanceGossip, expectedSize int) *notifyTracker {
	return &notifyTracker{
		logger:       logger,
		malCh:        mch,
		notifies:     make(map[string]*types.HareProofMsg, expectedSize),
		tracker:      NewRefCountTracker(),
		certificates: make(map[types.Hash32]struct{}, expectedSize),
	}
}

// OnNotify tracks the provided notification message.
// Returns true if the InnerMsg didn't affect the state, false otherwise.
func (nt *notifyTracker) OnNotify(ctx context.Context, msg *Msg) bool {
	pub := msg.PubKey
	eligibilityCount := uint32(msg.Eligibility.Count)
	if prev, exist := nt.notifies[pub.String()]; exist { // already seenSenders
		if prev.InnerMsg.Layer == msg.Layer &&
			prev.InnerMsg.Round == msg.Round &&
			prev.InnerMsg.MsgHash != msg.MsgHash {
			nt.logger.WithContext(ctx).With().Warning("equivocation detected at notify round", types.BytesToNodeID(pub.Bytes()))
			this := &types.HareProofMsg{
				InnerMsg:  msg.HareMetadata,
				Signature: msg.Signature,
			}
			if err := reportEquivocation(ctx, msg.PubKey.Bytes(), prev, this, &msg.Eligibility, nt.malCh); err != nil {
				nt.logger.WithContext(ctx).With().Warning("failed to report equivocation in notify round",
					types.BytesToNodeID(pub.Bytes()),
					log.Err(err))
			}
		}
		return true // ignored
	}

	// keep msg for pub
	nt.notifies[pub.String()] = &types.HareProofMsg{
		InnerMsg:  msg.HareMetadata,
		Signature: msg.Signature,
	}

	// track that set
	s := NewSet(msg.InnerMsg.Values)
	nt.onCertificate(msg.InnerMsg.Cert.AggMsgs.Messages[0].Round, s)
	nt.tracker.Track(s.ID(), eligibilityCount)

	return false
}

// NotificationsCount returns the number of notifications tracked for the provided set.
func (nt *notifyTracker) NotificationsCount(s *Set) int {
	return int(nt.tracker.CountStatus(s.ID()))
}

// calculates a unique id for the provided k and set.
func calcID(k uint32, set *Set) types.Hash32 {
	h := hash.New()

	// write K
	buff := make([]byte, 4)
	binary.LittleEndian.PutUint32(buff, k) // K>=0 because this not pre-round
	h.Write(buff)
	// write set ObjectID
	h.Write(set.ID().Bytes())

	return types.BytesToHash(h.Sum([]byte{}))
}

// tracks certificates.
func (nt *notifyTracker) onCertificate(k uint32, set *Set) {
	nt.certificates[calcID(k, set)] = struct{}{}
}

// HasCertificate returns true if a certificate exist for the provided set in the provided round, false otherwise.
func (nt *notifyTracker) HasCertificate(k uint32, set *Set) bool {
	_, exist := nt.certificates[calcID(k, set)]
	return exist
}
