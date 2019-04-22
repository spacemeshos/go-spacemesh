package hare

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/hare/metrics"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type commitTracker interface {
	OnCommit(msg *Msg)
	HasEnoughCommits() bool
	BuildCertificate() *Certificate
}

// Tracks commit Messages
type CommitTracker struct {
	seenSenders map[string]bool // tracks seen senders
	commits     []*XDRMessage   // tracks Set->Commits
	proposedSet *Set            // follows the set who has max number of commits
	threshold   int             // the number of required commits
}

func NewCommitTracker(threshold int, expectedSize int, proposedSet *Set) *CommitTracker {
	ct := &CommitTracker{}
	ct.seenSenders = make(map[string]bool, expectedSize)
	ct.commits = make([]*XDRMessage, 0, threshold)
	ct.proposedSet = proposedSet
	ct.threshold = threshold

	return ct
}

// Tracks the provided commit Message
func (ct *CommitTracker) OnCommit(msg *Msg) {
	if ct.proposedSet == nil { // no valid proposed set
		return
	}

	if ct.HasEnoughCommits() {
		return
	}

	pub := signing.NewPublicKey(msg.PubKey)
	if ct.seenSenders[pub.String()] {
		return
	}

	ct.seenSenders[pub.String()] = true

	s := NewSet(msg.Message.Values)
	if !ct.proposedSet.Equals(s) { // ignore commit on different set
		return
	}

	// add msg
	metrics.CommitCounter.With("set_id", fmt.Sprint(s.Id())).Add(1)
	ct.commits = append(ct.commits, msg.XDRMessage)
}

// Checks if the tracker received enough commits to build a certificate
func (ct *CommitTracker) HasEnoughCommits() bool {
	if ct.proposedSet == nil {
		return false
	}

	return len(ct.commits) >= ct.threshold
}

// Builds the certificate
// Returns the certificate if has enough commit Messages, nil otherwise
func (ct *CommitTracker) BuildCertificate() *Certificate {
	if !ct.HasEnoughCommits() {
		return nil
	}

	c := &Certificate{}
	c.Values = ct.proposedSet.To2DSlice()
	c.AggMsgs = &AggregatedMessages{}
	c.AggMsgs.Messages = ct.commits[:ct.threshold]

	// optimize msg size by setting Values to nil
	for _, commit := range c.AggMsgs.Messages {
		commit.Message.Values = nil
	}

	// TODO: set c.AggMsgs.AggSig

	return c
}
