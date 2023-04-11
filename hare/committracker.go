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
	seenSenders map[types.NodeID]*types.HareProofMsg // tracks seen senders
	committed   map[types.NodeID]struct{}
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
		seenSenders: make(map[types.NodeID]*types.HareProofMsg, expectedSize),
		committed:   make(map[types.NodeID]struct{}, expectedSize),
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

	metadata := types.HareMetadata{
		Layer:   msg.Layer,
		Round:   msg.Round,
		MsgHash: types.BytesToHash(msg.Message.HashBytes()),
	}
	if prev, ok := ct.seenSenders[msg.SmesherID]; ok {
		if !prev.InnerMsg.Equivocation(&metadata) {
			return
		}

		ct.logger.WithContext(ctx).With().Warning("equivocation detected in commit round",
			log.Stringer("smesher", msg.SmesherID),
			log.Object("prev", &prev.InnerMsg),
			log.Object("curr", &metadata),
		)
		ct.eTracker.Track(msg.SmesherID, msg.Round, msg.Eligibility.Count, false)
		this := &types.HareProofMsg{
			InnerMsg:  metadata,
			Signature: msg.Signature,
		}
		if err := reportEquivocation(ctx, msg.SmesherID, prev, this, &msg.Eligibility, ct.malCh); err != nil {
			ct.logger.WithContext(ctx).With().Warning("failed to report equivocation in commit round",
				log.Stringer("smesher", msg.SmesherID),
				log.Err(err),
			)
			return
		}
		return
	}
	ct.seenSenders[msg.SmesherID] = &types.HareProofMsg{
		InnerMsg:  metadata,
		Signature: msg.Signature,
	}

	s := NewSet(msg.Values)
	if !ct.proposedSet.Equals(s) { // ignore commit on different set
		return
	}

	// add msg
	ct.commits = append(ct.commits, msg.Message)
	ct.committed[msg.SmesherID] = struct{}{}
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
	ct.eTracker.ForEach(ct.round, func(node types.NodeID, cr *Cred) {
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
		commit.Values = nil
	}

	// TODO: set c.AggMsgs.AggSig

	return c
}
