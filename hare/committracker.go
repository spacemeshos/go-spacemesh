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
	CommitCount() int
}

// commitTracker tracks commit messages and build the certificate according to the tracked messages.
type commitTracker struct {
	logger           log.Log
	malCh            chan types.MalfeasanceGossip
	seenSenders      map[string]*types.HareProofMsg // tracks seen senders
	commits          []Message                      // tracks Set->Commits
	proposedSet      *Set                           // follows the set who has max number of commits
	threshold        int                            // the number of required commits
	eligibilityCount int
}

func newCommitTracker(logger log.Log, mch chan types.MalfeasanceGossip, threshold, expectedSize int, proposedSet *Set) *commitTracker {
	return &commitTracker{
		malCh:       mch,
		logger:      logger,
		seenSenders: make(map[string]*types.HareProofMsg, expectedSize),
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

	pub := msg.PubKey
	if prev, ok := ct.seenSenders[pub.String()]; ok {
		if prev.InnerMsg.Layer == msg.Layer &&
			prev.InnerMsg.Round == msg.Round &&
			prev.InnerMsg.MsgHash != msg.MsgHash {
			ct.logger.WithContext(ctx).With().Warning("equivocation detected at commit round", types.BytesToNodeID(pub.Bytes()))
			this := &types.HareProofMsg{
				InnerMsg:  msg.HareMetadata,
				Signature: msg.Signature,
			}
			if err := reportEquivocation(ctx, msg.PubKey.Bytes(), prev, this, &msg.Eligibility, ct.malCh); err != nil {
				ct.logger.WithContext(ctx).With().Warning("failed to report equivocation in commit round",
					types.BytesToNodeID(pub.Bytes()),
					log.Err(err))
				return
			}
		}
		return
	}

	ct.seenSenders[pub.String()] = &types.HareProofMsg{
		InnerMsg:  msg.HareMetadata,
		Signature: msg.Signature,
	}

	s := NewSet(msg.InnerMsg.Values)
	if !ct.proposedSet.Equals(s) { // ignore commit on different set
		return
	}

	// add msg
	ct.commits = append(ct.commits, msg.Message)
	ct.eligibilityCount += int(msg.Eligibility.Count)
}

// HasEnoughCommits returns true if the tracker can build a certificate, false otherwise.
func (ct *commitTracker) HasEnoughCommits() bool {
	if ct.proposedSet == nil {
		return false
	}
	return ct.eligibilityCount >= ct.threshold
}

func (ct *commitTracker) CommitCount() int {
	return ct.eligibilityCount
}

// BuildCertificate returns a certificate if there are enough commits, nil otherwise
// Returns the certificate if has enough commit Messages, nil otherwise.
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
