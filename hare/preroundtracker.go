package hare

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
)

type preroundData struct {
	*Set
	*types.HareProofMsg
}

// preRoundTracker tracks pre-round messages.
// The tracker can be queried to check if a value or a set is provable.
// It also provides the ability to filter set from unprovable values.
type preRoundTracker struct {
	logger    log.Log
	malCh     chan types.MalfeasanceGossip
	preRound  map[string]*preroundData // maps PubKey->Set of already tracked Values
	tracker   *RefCountTracker         // keeps track of seen Values
	threshold uint32                   // the threshold to prove a value
	bestVRF   uint32                   // the lowest VRF value seen in the round
	coinflip  bool                     // the value of the weak coin (based on bestVRF)
}

func newPreRoundTracker(logger log.Log, mch chan types.MalfeasanceGossip, threshold, expectedSize int) *preRoundTracker {
	return &preRoundTracker{
		logger:    logger,
		malCh:     mch,
		preRound:  make(map[string]*preroundData, expectedSize),
		tracker:   NewRefCountTracker(),
		threshold: uint32(threshold),
		bestVRF:   math.MaxUint32,
	}
}

// OnPreRound tracks pre-round messages.
func (pre *preRoundTracker) OnPreRound(ctx context.Context, msg *Msg) {
	logger := pre.logger.WithContext(ctx)

	pub := msg.PubKey

	// check for winning VRF
	sha := hash.Sum(msg.Eligibility.Proof)
	shaUint32 := binary.LittleEndian.Uint32(sha[:4])
	logger.With().Debug("received preround message",
		log.String("sender_id", pub.ShortString()),
		log.Int("num_values", len(msg.InnerMsg.Values)),
		log.String("vrf_value", fmt.Sprintf("%x", shaUint32)))
	// TODO: make sure we don't need a mutex here
	if shaUint32 < pre.bestVRF {
		pre.bestVRF = shaUint32
		// store lowest-order bit as coin toss value
		pre.coinflip = sha[0]&byte(1) == byte(1)
		pre.logger.With().Debug("got new best vrf value",
			log.String("sender_id", pub.ShortString()),
			log.String("vrf_value", fmt.Sprintf("%x", shaUint32)),
			log.Bool("weak_coin", pre.coinflip))
	}

	eligibilityCount := uint32(msg.Eligibility.Count)
	sToTrack := NewSet(msg.InnerMsg.Values) // assume track all Values
	alreadyTracked := NewDefaultEmptySet()  // assume nothing tracked so far

	if prev, exist := pre.preRound[pub.String()]; exist { // not first pre-round msg from this sender
		if prev.InnerMsg.Layer == msg.Layer &&
			prev.InnerMsg.Round == msg.Round &&
			prev.InnerMsg.MsgHash != msg.MsgHash {
			pre.logger.WithContext(ctx).With().Warning("equivocation detected at preround", types.BytesToNodeID(pub.Bytes()))
			this := &types.HareProofMsg{
				InnerMsg:  msg.HareMetadata,
				Signature: msg.Signature,
			}
			if err := reportEquivocation(ctx, msg.PubKey.Bytes(), prev.HareProofMsg, this, &msg.Eligibility, pre.malCh); err != nil {
				pre.logger.WithContext(ctx).With().Warning("failed to report equivocation in preround",
					types.BytesToNodeID(pub.Bytes()),
					log.Err(err))
				return
			}
		}
		logger.With().Debug("duplicate preround msg sender", log.String("sender_id", pub.ShortString()))
		alreadyTracked = prev.Set         // update already tracked Values
		sToTrack.Subtract(alreadyTracked) // subtract the already tracked Values
	} else {
		pre.preRound[pub.String()] = &preroundData{}
	}

	// record Values
	for _, v := range sToTrack.ToSlice() {
		pre.tracker.Track(v, eligibilityCount)
	}

	// update the union to include new Values
	pre.preRound[pub.String()].Set = alreadyTracked.Union(sToTrack)
	pre.preRound[pub.String()].HareProofMsg = &types.HareProofMsg{
		InnerMsg:  msg.HareMetadata,
		Signature: msg.Signature,
	}
}

// CanProveValue returns true if the given value is provable, false otherwise.
// a value is said to be provable if it has at least threshold pre-round votes to support it.
func (pre *preRoundTracker) CanProveValue(value types.ProposalID) bool {
	// at least threshold occurrences of a given value
	countStatus := pre.tracker.CountStatus(value)
	pre.logger.With().Debug("preround tracker count",
		value,
		log.Uint32("count", countStatus),
		log.Uint32("threshold", pre.threshold))
	return countStatus >= pre.threshold
}

// CanProveSet returns true if the give set is provable, false otherwise.
// a set is said to be provable if all his values are provable.
func (pre *preRoundTracker) CanProveSet(set *Set) bool {
	// a set is provable iff all its Values are provable
	for _, bid := range set.ToSlice() {
		if !pre.CanProveValue(bid) {
			return false
		}
	}

	return true
}

// FilterSet filters out non-provable values from the given set.
func (pre *preRoundTracker) FilterSet(set *Set) {
	for _, v := range set.ToSlice() {
		if !pre.CanProveValue(v) { // not enough witnesses
			set.Remove(v)
		}
	}
}
