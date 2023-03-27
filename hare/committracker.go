package hare

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type commitTrackerProvider interface {
	OnCommit(context.Context, *Msg)
	HasEnoughCommits() bool
	BuildCertificate() *Certificate
	CommitCount() *CountInfo
}

// commitTracker tracks commit messages and build the certificate according to the tracked messages.
type commitTracker struct {
	logger      log.Log
	round       uint32
	malCh       chan<- *types.MalfeasanceGossip
	seenSenders map[string]*types.HareProofMsg // tracks seen senders
	committed   map[string]struct{}
	commits     []Message // tracks Set->Commits
	proposedSet *Set      // follows the set who has max number of commits
	threshold   int       // the number of required commits
	eTracker    *EligibilityTracker
	finalTally  *CountInfo
}

func newCommitTracker(
	logger log.Log,
	round uint32,
	mch chan<- *types.MalfeasanceGossip,
	et *EligibilityTracker,
	threshold, expectedSize int,
	proposedSet *Set,
) *commitTracker {
	return &commitTracker{
		logger:      logger,
		round:       round,
		malCh:       mch,
		eTracker:    et,
		seenSenders: make(map[string]*types.HareProofMsg, expectedSize),
		committed:   make(map[string]struct{}, expectedSize),
		commits:     make([]Message, 0, threshold),
		proposedSet: proposedSet,
		threshold:   threshold,
	}
}

// OnCommit tracks the given commit message.
func (ct *commitTracker) OnCommit(ctx context.Context, msg *Msg) {
	if ct.proposedSet == nil { // no valid proposed set
		return
	}

	if ct.HasEnoughCommits() {
		return
	}

	if prev, ok := ct.seenSenders[string(msg.NodeID.Bytes())]; ok {
		if prev.InnerMsg.Layer == msg.Layer &&
			prev.InnerMsg.Round == msg.Round &&
			prev.InnerMsg.MsgHash != msg.MsgHash {
			nodeID := types.BytesToNodeID(msg.NodeID.Bytes())
			ct.logger.WithContext(ctx).With().Warning("equivocation detected in commit round",
				log.Stringer("smesher", nodeID),
				log.Object("prev", &prev.InnerMsg),
				log.Object("curr", &msg.HareMetadata),
			)
			ct.eTracker.Track(msg.NodeID.Bytes(), msg.Round, msg.Eligibility.Count, false)
			this := &types.HareProofMsg{
				InnerMsg:  msg.HareMetadata,
				Signature: msg.Signature,
			}
			if err := reportEquivocation(ctx, msg.NodeID.Bytes(), prev, this, &msg.Eligibility, ct.malCh); err != nil {
				ct.logger.WithContext(ctx).With().Warning("failed to report equivocation in commit round",
					log.Stringer("smesher", nodeID),
					log.Err(err))
				return
			}
		}
		return
	}
	ct.seenSenders[string(msg.NodeID.Bytes())] = &types.HareProofMsg{
		InnerMsg:  msg.HareMetadata,
		Signature: msg.Signature,
	}

	s := NewSet(msg.InnerMsg.Values)
	if !ct.proposedSet.Equals(s) { // ignore commit on different set
		return
	}

	// add msg
	ct.commits = append(ct.commits, msg.Message)
	ct.committed[string(msg.NodeID.Bytes())] = struct{}{}
}

// HasEnoughCommits returns true if the tracker can build a certificate, false otherwise.
func (ct *commitTracker) HasEnoughCommits() bool {
	if ct.proposedSet == nil {
		return false
	}
	if ct.finalTally != nil {
		return true
	}
	ci := ct.CommitCount()
	enough := ci.Meet(ct.threshold)
	if enough {
		ct.finalTally = ci
		if ci.numDishonest > 0 {
			ct.logger.With().Warning("counting votes from malicious identities",
				log.Object("eligibility_count", ci))
		} else {
			ct.logger.With().Info("commit round completed", log.Object("eligibility_count", ci))
		}
	}
	return enough
}

// CommitCount tallies the eligibility count for the commit messages on the proposed set.
// if it crosses the threshold, cache it.
func (ct *commitTracker) CommitCount() *CountInfo {
	if ct.finalTally != nil {
		return ct.finalTally
	}
	var ci CountInfo
	ct.eTracker.ForEach(ct.round, func(node string, cr *Cred) {
		// only counts the eligibility count from the committed msgs
		if _, ok := ct.committed[node]; ok {
			if cr.Honest {
				ci.IncHonest(cr.Count)
			} else {
				ci.IncDishonest(cr.Count)
			}
		} else if !cr.Honest {
			ci.IncKnownEquivocator(cr.Count)
		}
	})
	return &ci
}

// BuildCertificate returns a certificate if there are enough commits, nil otherwise
// Returns the certificate if it has enough commit Messages, nil otherwise.
func (ct *commitTracker) BuildCertificate() *Certificate {
	if !ct.HasEnoughCommits() {
		return nil
	}

	c := &Certificate{}
	c.Values = ct.proposedSet.ToSlice()
	c.AggMsgs = &AggregatedMessages{}
	c.AggMsgs.Messages = ct.commits // just enough to fit eligibility threshold

	// optimize msg size by setting Values to nil
	for _, commit := range c.AggMsgs.Messages {
		commit.InnerMsg.Values = nil
	}

	// TODO: set c.AggMsgs.AggSig

	return c
}
