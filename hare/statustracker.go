package hare

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// statusTracker tracks status messages.
// Provides functions to build a proposal and validate the statuses.
type statusTracker struct {
	logger            log.Log
	round             uint32
	malCh             chan<- *types.MalfeasanceGossip
	statuses          map[string]*Msg // maps PubKey->StatusMsg
	threshold         int             // threshold to indicate a set can be proved
	maxCommittedRound uint32          // tracks max committed round (Ki) value in tracked status Messages
	maxSet            *Set            // tracks the max raw set in the tracked status Messages
	analyzed          bool            // indicates if the Messages have already been analyzed
	eTracker          *EligibilityTracker
	tally             *CountInfo
}

func newStatusTracker(logger log.Log, round uint32, mch chan<- *types.MalfeasanceGossip, et *EligibilityTracker, threshold, expectedSize int) *statusTracker {
	return &statusTracker{
		logger:            logger,
		malCh:             mch,
		round:             round,
		statuses:          make(map[string]*Msg, expectedSize),
		threshold:         threshold,
		maxCommittedRound: preRound,
		eTracker:          et,
	}
}

// RecordStatus records the given status message.
func (st *statusTracker) RecordStatus(ctx context.Context, msg *Msg) {
	nodeID := types.BytesToNodeID(msg.PubKey.Bytes())
	if prev, exist := st.statuses[string(msg.PubKey.Bytes())]; exist { // already handled this sender's status msg
		if prev.Layer == msg.Layer &&
			prev.Round == msg.Round &&
			prev.MsgHash != msg.MsgHash {
			st.logger.WithContext(ctx).With().Warning("equivocation detected at status round",
				log.String("sender_id", nodeID.ShortString()))
			st.eTracker.Track(msg.PubKey.Bytes(), msg.Round, msg.Eligibility.Count, false)
			old := &types.HareProofMsg{
				InnerMsg:  prev.HareMetadata,
				Signature: prev.Signature,
			}
			this := &types.HareProofMsg{
				InnerMsg:  msg.HareMetadata,
				Signature: msg.Signature,
			}
			if err := reportEquivocation(ctx, msg.PubKey.Bytes(), old, this, &msg.Eligibility, st.malCh); err != nil {
				st.logger.WithContext(ctx).With().Warning("failed to report equivocation in status round",
					log.String("sender_id", nodeID.ShortString()),
					log.Err(err))
				return
			}
		}
		st.logger.WithContext(ctx).With().Warning("duplicate status message detected",
			log.String("sender_id", nodeID.ShortString()))
		return
	}

	st.statuses[string(msg.PubKey.Bytes())] = msg
}

// AnalyzeStatusMessages analyzes the recorded status messages by the validation function.
func (st *statusTracker) AnalyzeStatusMessages(isValid func(m *Msg) bool) {
	for key, m := range st.statuses {
		if !isValid(m) { // only keep valid Messages
			delete(st.statuses, key)
		} else {
			if m.InnerMsg.CommittedRound >= st.maxCommittedRound || st.maxCommittedRound == preRound { // track max Ki & matching raw set
				st.maxCommittedRound = m.InnerMsg.CommittedRound
				st.maxSet = NewSet(m.InnerMsg.Values)
			}
		}
	}

	// only valid messages are left now
	var ci CountInfo
	st.eTracker.ForEach(st.round, func(node string, cr *Cred) {
		// only counts the eligibility count from the committed msgs
		if _, ok := st.statuses[node]; ok {
			if cr.Honest {
				ci.IncHonest(cr.Count)
			} else {
				ci.IncDishonest(cr.Count)
			}
		} else if !cr.Honest {
			ci.IncKnownEquivocator(cr.Count)
		}
	})
	st.tally = &ci
	st.analyzed = true
}

// IsSVPReady returns true if there are enough statuses to build an SVP, false otherwise.
func (st *statusTracker) IsSVPReady() bool {
	return st.analyzed && st.tally.Meet(st.threshold)
}

// ProposalSet returns the proposed set if available, nil otherwise.
func (st *statusTracker) ProposalSet(expectedSize int) *Set {
	if st.maxCommittedRound == preRound {
		return st.buildUnionSet(expectedSize)
	}

	if st.maxSet == nil { // should be impossible to reach
		log.Fatal("maxSet is unexpectedly nil")
	}

	return st.maxSet
}

// returns the union set of all status Messages collected.
func (st *statusTracker) buildUnionSet(expectedSize int) *Set {
	unionSet := NewEmptySet(expectedSize)
	for _, m := range st.statuses {
		for _, v := range NewSet(m.InnerMsg.Values).ToSlice() {
			unionSet.Add(v) // assuming add is unique
		}
	}

	return unionSet
}

// BuildSVP builds the SVP if available and returns it, it return false otherwise.
func (st *statusTracker) BuildSVP() *AggregatedMessages {
	if !st.IsSVPReady() {
		return nil
	}

	svp := &AggregatedMessages{}
	for _, m := range st.statuses {
		svp.Messages = append(svp.Messages, m.Message)
	}

	// TODO: set aggregated signature
	if len(svp.Messages) == 0 {
		return nil
	}

	return svp
}
