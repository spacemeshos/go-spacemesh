package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

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

func (ct *CommitTracker) OnCommit(msg *pb.HareMessage) {
	if ct.proposedSet == nil { // no valid proposed set
		return
	}

	if ct.HasEnoughCommits() {
		return
	}

	pub, err := crypto.NewPublicKey(msg.PubKey)
	if err != nil {
		log.Warning("Could not construct public key: ", err.Error())
		return
	}

	if ct.seenSenders[pub.String()] {
		return
	}

	ct.seenSenders[pub.String()] = true

	s := NewSet(msg.Message.Values)
	if !ct.proposedSet.Equals(s) { // ignore commit on different set
		return
	}

	// add msg
	ct.commits = append(ct.commits, msg)
}

func (ct *CommitTracker) HasEnoughCommits() bool {
	if ct.proposedSet == nil {
		return false
	}

	return len(ct.commits) >= ct.threshold
}

func (ct *CommitTracker) BuildCertificate() *pb.Certificate {
	if !ct.HasEnoughCommits() {
		return nil
	}

	c := &pb.Certificate{}
	c.Values = ct.proposedSet.To2DSlice()
	c.AggMsgs = &pb.AggregatedMessages{}
	c.AggMsgs.Messages = ct.commits

	// optimize msg size by setting values to nil
	for _, commit := range c.AggMsgs.Messages {
		commit.Message.Values = nil
	}

	// TODO: set c.AggMsgs.AggSig

	return c
}
