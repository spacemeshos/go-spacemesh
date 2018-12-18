package hare

import (
	"bytes"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
)

type ProposalTracker struct {
	proposal      *pb.HareMessage
	isConflicting bool
}

func NewProposalTracker() ProposalTracker {
	pt := ProposalTracker{}
	pt.proposal = nil
	pt.isConflicting = false

	return pt
}

func (pt *ProposalTracker) OnProposal(msg *pb.HareMessage) {
	if pt.proposal == nil {
		pt.proposal = msg
		return
	}

	s := NewSet(msg.Message.Blocks)
	g := NewSet(pt.proposal.Message.Blocks)
	if bytes.Equal(msg.PubKey, pt.proposal.PubKey) && !s.Equals(g) {
		// same pubKey, not same set
		pt.isConflicting = true
	}
}

func (pt *ProposalTracker) HasValidProposal() bool {
	return pt.isConflicting
}

func (pt *ProposalTracker) ProposedSet() *Set {
	return NewSet(pt.proposal.Message.Blocks)
}
