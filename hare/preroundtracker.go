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
	malCh     chan<- *types.MalfeasanceGossip
	preRound  map[types.NodeID]*preroundData // maps PubKey->Set of already tracked Values
	tracker   *RefCountTracker               // keeps track of seen Values
	threshold int                            // the threshold to prove a value
	bestVRF   uint32                         // the lowest VRF value seen in the round
	coinflip  bool                           // the value of the weak coin (based on bestVRF)
	eTracker  *EligibilityTracker
}

func newPreRoundTracker(logger log.Log, mch chan<- *types.MalfeasanceGossip, et *EligibilityTracker, threshold, expectedSize int) *preRoundTracker {
	return &preRoundTracker{
		logger:    logger,
		malCh:     mch,
		preRound:  make(map[types.NodeID]*preroundData, expectedSize),
		tracker:   NewRefCountTracker(preRound, et, expectedSize),
		threshold: threshold,
		bestVRF:   math.MaxUint32,
		eTracker:  et,
	}
}

// OnPreRound tracks pre-round messages.
func (pre *preRoundTracker) OnPreRound(ctx context.Context, msg *Msg) {
	logger := pre.logger.WithContext(ctx)

	// check for winning VRF
	vrfHash := hash.Sum(msg.Eligibility.Proof)
	vrfHashVal := binary.LittleEndian.Uint32(vrfHash[:4])
	logger.With().Debug("received preround message",
		msg.NodeID,
		log.Stringer("smesher", msg.NodeID),
		log.Int("num_values", len(msg.InnerMsg.Values)),
		log.Uint32("vrf_value", vrfHashVal),
	)
	if vrfHashVal < pre.bestVRF {
		pre.bestVRF = vrfHashVal
		// store lowest-order bit as coin toss value
		pre.coinflip = vrfHash[0]&byte(1) == byte(1)
		pre.logger.With().Debug("got new best vrf value",
			log.Stringer("smesher", msg.NodeID),
			log.String("vrf_value", fmt.Sprintf("%x", vrfHashVal)),
			log.Bool("weak_coin", pre.coinflip),
		)
	}

	sToTrack := NewSet(msg.InnerMsg.Values) // assume track all Values
	alreadyTracked := NewDefaultEmptySet()  // assume nothing tracked so far

	if prev, exist := pre.preRound[msg.NodeID]; exist { // not first pre-round msg from this sender
		if prev.InnerMsg.Layer == msg.Layer &&
			prev.InnerMsg.Round == msg.Round &&
			prev.InnerMsg.MsgHash != msg.MsgHash {
			pre.logger.WithContext(ctx).With().Warning("equivocation detected in preround",
				log.Stringer("smesher", msg.NodeID),
				log.Object("prev", &prev.InnerMsg),
				log.Object("curr", &msg.HareMetadata),
			)
			pre.eTracker.Track(msg.NodeID, msg.Round, msg.Eligibility.Count, false)
			this := &types.HareProofMsg{
				InnerMsg:  msg.HareMetadata,
				Signature: msg.Signature,
			}
			if err := reportEquivocation(ctx, msg.NodeID, prev.HareProofMsg, this, &msg.Eligibility, pre.malCh); err != nil {
				pre.logger.WithContext(ctx).With().Warning("failed to report equivocation in preround",
					log.Stringer("smesher", msg.NodeID),
					log.Err(err),
				)
				return
			}
		}
		logger.With().Debug("duplicate preround msg sender", log.Stringer("smesher", msg.NodeID))
		alreadyTracked = prev.Set         // update already tracked Values
		sToTrack.Subtract(alreadyTracked) // subtract the already tracked Values
	} else {
		pre.preRound[msg.NodeID] = &preroundData{}
	}

	// record Values
	for _, v := range sToTrack.ToSlice() {
		pre.tracker.Track(v, msg.NodeID)
	}

	// update the union to include new Values
	pre.preRound[msg.NodeID].Set = alreadyTracked.Union(sToTrack)
	pre.preRound[msg.NodeID].HareProofMsg = &types.HareProofMsg{
		InnerMsg:  msg.HareMetadata,
		Signature: msg.Signature,
	}
}

// CanProveValue returns true if the given value is provable, false otherwise.
// a value is said to be provable if it has at least threshold pre-round votes to support it.
func (pre *preRoundTracker) CanProveValue(value types.ProposalID) bool {
	// at least threshold occurrences of a given value
	countStatus := pre.tracker.CountStatus(value)
	if countStatus == nil {
		pre.logger.Fatal("unexpected count")
	}
	if countStatus.numDishonest > 0 {
		pre.logger.With().Warning("counting votes from malicious identities", value, log.Inline(countStatus))
	}
	pre.logger.With().Debug("preround tracker count",
		value,
		log.Inline(countStatus),
		log.Int("threshold", pre.threshold))
	return countStatus.Meet(pre.threshold)
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
