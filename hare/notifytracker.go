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
	round        uint32
	malCh        chan<- *types.MalfeasanceGossip
	notifies     map[types.NodeID]*types.HareProofMsg // tracks PubKey->Notification
	tracker      *RefCountTracker                     // tracks ref count to each seen set
	eTracker     *EligibilityTracker
	certificates map[types.Hash32]struct{} // tracks Set->certificate
}

func newNotifyTracker(
	logger log.Log,
	round uint32,
	mch chan<- *types.MalfeasanceGossip,
	et *EligibilityTracker,
	expectedSize int,
) *notifyTracker {
	return &notifyTracker{
		logger:       logger,
		round:        round,
		malCh:        mch,
		notifies:     make(map[types.NodeID]*types.HareProofMsg, expectedSize),
		tracker:      NewRefCountTracker(round, et, expectedSize),
		eTracker:     et,
		certificates: make(map[types.Hash32]struct{}, expectedSize),
	}
}

// OnNotify tracks the provided notification message.
// Returns true if the InnerMsg didn't affect the state, false otherwise.
func (nt *notifyTracker) OnNotify(ctx context.Context, msg *Message) bool {
	metadata := types.HareMetadata{
		Layer:   msg.Layer,
		Round:   msg.Round,
		MsgHash: types.BytesToHash(msg.HashBytes()),
	}

	if prev, exist := nt.notifies[msg.SmesherID]; exist { // already seenSenders
		if !prev.InnerMsg.Equivocation(&metadata) {
			return true // ignored
		}

		nt.logger.WithContext(ctx).With().Warning("equivocation detected in notify round",
			log.Stringer("smesher", msg.SmesherID),
			log.Object("prev", &prev.InnerMsg),
			log.Object("curr", &metadata),
		)
		nt.eTracker.Track(msg.SmesherID, msg.Round, msg.Eligibility.Count, false)
		this := &types.HareProofMsg{
			InnerMsg:  metadata,
			Signature: msg.Signature,
		}
		if err := reportEquivocation(ctx, msg.SmesherID, prev, this, &msg.Eligibility, nt.malCh); err != nil {
			nt.logger.WithContext(ctx).With().Warning("failed to report equivocation in notify round",
				log.Stringer("smesher", msg.SmesherID),
				log.Err(err),
			)
		}
		return true // ignored
	}

	// keep msg for pub
	nt.notifies[msg.SmesherID] = &types.HareProofMsg{
		InnerMsg:  metadata,
		Signature: msg.Signature,
	}

	// track that set
	s := NewSet(msg.Values)
	nt.onCertificate(msg.Cert.AggMsgs.Messages[0].Round, s)
	nt.tracker.Track(s.ID(), msg.SmesherID)
	return false
}

// NotificationsCount returns the number of notifications tracked for the provided set.
func (nt *notifyTracker) NotificationsCount(s *Set) *CountInfo {
	return nt.tracker.CountStatus(s.ID())
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
