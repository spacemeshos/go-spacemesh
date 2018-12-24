package hare

import (
	"bytes"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
)

type ProposalTracker struct {
	proposal      *pb.HareMessage // maps PubKey->Proposal
	isConflicting bool            // maps PubKey->ConflictStatus
}

func NewProposalTracker(expectedSize int) *ProposalTracker {
	pt := &ProposalTracker{}
	pt.proposal = nil
	pt.isConflicting = false

	return pt
}

func (pt *ProposalTracker) OnProposal(msg *pb.HareMessage) {
	if pt.proposal == nil { // first leader
		pt.proposal = msg // just update
		return
	}

	// if same sender then we should check for equivocation
	if bytes.Equal(pt.proposal.PubKey, msg.PubKey) {
		s := NewSet(msg.Message.Values)
		g := NewSet(pt.proposal.Message.Values)
		if !s.Equals(g) { // equivocation detected
			pt.isConflicting = true
		}

		return
	}

	// TODO: ignore msgs with higher ranked role proof

	pt.proposal = msg // update leader
}

func (pt *ProposalTracker) IsConflicting() bool {
	return pt.isConflicting
}

func (pt *ProposalTracker) ProposedSet() (*Set, bool) {
	if pt.proposal == nil {
		return nil, false
	}

	if pt.isConflicting {
		return nil, false
	}

	return NewSet(pt.proposal.Message.Values), true
}
