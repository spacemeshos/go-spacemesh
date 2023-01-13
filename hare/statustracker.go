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
	malCh             chan types.MalfeasanceGossip
	statuses          map[string]*Msg // maps PubKey->StatusMsg
	threshold         uint16          // threshold to indicate a set can be proved
	maxCommittedRound uint32          // tracks max committed round (Ki) value in tracked status Messages
	maxSet            *Set            // tracks the max raw set in the tracked status Messages
	count             uint16          // the count of valid status messages
	analyzed          bool            // indicates if the Messages have already been analyzed
}

func newStatusTracker(logger log.Log, mch chan types.MalfeasanceGossip, threshold, expectedSize int) *statusTracker {
	return &statusTracker{
		malCh:             mch,
		logger:            logger,
		statuses:          make(map[string]*Msg, expectedSize),
		threshold:         uint16(threshold),
		maxCommittedRound: preRound,
	}
}

// RecordStatus records the given status message.
func (st *statusTracker) RecordStatus(ctx context.Context, msg *Msg) {
	pub := msg.PubKey
	if prev, exist := st.statuses[pub.String()]; exist { // already handled this sender's status msg
		if prev.Layer == msg.Layer &&
			prev.Round == msg.Round &&
			prev.MsgHash != msg.MsgHash {
			st.logger.WithContext(ctx).With().Warning("equivocation detected at status round", types.BytesToNodeID(pub.Bytes()))
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
					types.BytesToNodeID(pub.Bytes()),
					log.Err(err))
				return
			}
		}
		st.logger.WithContext(ctx).With().Warning("duplicate status message detected", types.BytesToNodeID(pub.Bytes()))
		return
	}

	st.statuses[pub.String()] = msg
}

// AnalyzeStatuses analyzes the recorded status messages by the validation function.
func (st *statusTracker) AnalyzeStatuses(isValid func(m *Msg) bool) {
	st.count = 0
	for key, m := range st.statuses {
		if !isValid(m) { // only keep valid Messages
			delete(st.statuses, key)
		} else {
			st.count += m.Eligibility.Count
			if m.InnerMsg.CommittedRound >= st.maxCommittedRound || st.maxCommittedRound == preRound { // track max Ki & matching raw set
				st.maxCommittedRound = m.InnerMsg.CommittedRound
				st.maxSet = NewSet(m.InnerMsg.Values)
			}
		}
	}

	st.analyzed = true
}

// IsSVPReady returns true if theere are enough statuses to build an SVP, false otherwise.
func (st *statusTracker) IsSVPReady() bool {
	return st.analyzed && st.count >= st.threshold
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
		for _, bid := range NewSet(m.InnerMsg.Values).elements() {
			unionSet.Add(bid) // assuming add is unique
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
