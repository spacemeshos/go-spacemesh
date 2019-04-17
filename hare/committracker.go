package hare

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/hare/metrics"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type commitTracker interface {
	OnCommit(msg *Msg)
	HasEnoughCommits() bool
	BuildCertificate() *pb.Certificate
}

// Tracks commit messages
type CommitTracker struct {
	seenSenders map[string]bool   // tracks seen senders
	commits     []*pb.HareMessage // tracks Set->Commits
	proposedSet *Set              // follows the set who has max number of commits
	threshold   int               // the number of required commits
}

func NewCommitTracker(threshold int, expectedSize int, proposedSet *Set) *CommitTracker {
	ct := &CommitTracker{}
	ct.seenSenders = make(map[string]bool, expectedSize)
	ct.commits = make([]*pb.HareMessage, 0, threshold)
	ct.proposedSet = proposedSet
	ct.threshold = threshold

	return ct
}

// Tracks the provided commit message
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
	ct.commits = append(ct.commits, msg.HareMessage)
}

// Checks if the tracker received enough commits to build a certificate
func (ct *CommitTracker) HasEnoughCommits() bool {
	if ct.proposedSet == nil {
		return false
	}

	return len(ct.commits) >= ct.threshold
}

// Builds the certificate
// Returns the certificate if has enough commit messages, nil otherwise
func (ct *CommitTracker) BuildCertificate() *pb.Certificate {
	if !ct.HasEnoughCommits() {
		return nil
	}

	c := &pb.Certificate{}
	c.Values = ct.proposedSet.To2DSlice()
	c.AggMsgs = &pb.AggregatedMessages{}
	c.AggMsgs.Messages = ct.commits[:ct.threshold]

	// optimize msg size by setting values to nil
	for _, commit := range c.AggMsgs.Messages {
		commit.Message.Values = nil
	}

	// TODO: set c.AggMsgs.AggSig

	return c
}
