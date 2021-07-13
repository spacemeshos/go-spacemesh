package hare

import (
	"bytes"
	"context"
	"github.com/spacemeshos/go-spacemesh/log"
)

type proposalTrackerProvider interface {
	OnProposal(context.Context, *Msg)
	OnLateProposal(context.Context, *Msg)
	IsConflicting() bool
	ProposedSet() *Set
}

// proposalTracker tracks proposal messages
type proposalTracker struct {
	log.Log
	proposal      *Msg // maps PubKey->Proposal
	isConflicting bool // maps PubKey->ConflictStatus
}

func newProposalTracker(log log.Log) *proposalTracker {
	pt := &proposalTracker{}
	pt.proposal = nil
	pt.isConflicting = false
	pt.Log = log

	return pt
}

// OnProposal tracks the provided proposal message.
// It assumes the proposal message is syntactically valid and that it was received on the proposal round.
func (pt *proposalTracker) OnProposal(ctx context.Context, msg *Msg) {
	if pt.proposal == nil { // first leader
		pt.proposal = msg // just update
		return
	}

	// if same sender then we should check for equivocation
	if pt.proposal.PubKey.Equals(msg.PubKey) {
		s := NewSet(msg.InnerMsg.Values)
		g := NewSet(pt.proposal.InnerMsg.Values)
		if !s.Equals(g) { // equivocation detected
			pt.WithContext(ctx).With().Info("equivocation detected on proposal round",
				log.String("id_malicious", msg.PubKey.String()),
				log.String("current_set", g.String()),
				log.String("conflicting_set", s.String()))
			pt.isConflicting = true
		}

		return // process done
	}

	// ignore msgs with higher ranked role proof
	if bytes.Compare(msg.InnerMsg.RoleProof, pt.proposal.InnerMsg.RoleProof) > 0 {
		return
	}

	pt.proposal = msg        // update lower leader msg
	pt.isConflicting = false // assume no conflict
}

// OnLateProposal tracks the given proposal message.
// It assumes the proposal message is syntactically valid and that it was not received on the proposal round (late).
func (pt *proposalTracker) OnLateProposal(ctx context.Context, msg *Msg) {
	if pt.proposal == nil {
		return
	}

	// if same sender then we should check for equivocation
	if pt.proposal.PubKey.Equals(msg.PubKey) {
		s := NewSet(msg.InnerMsg.Values)
		g := NewSet(pt.proposal.InnerMsg.Values)
		if !s.Equals(g) { // equivocation detected
			pt.WithContext(ctx).With().Info("equivocation detected for late round",
				log.String("id_malicious", msg.PubKey.String()),
				log.String("current_set", g.String()),
				log.String("conflicting_set", s.String()))
			pt.isConflicting = true
		}
	}

	// not equal check rank
	// lower ranked proposal on late proposal is a conflict
	if bytes.Compare(msg.InnerMsg.RoleProof, pt.proposal.InnerMsg.RoleProof) < 0 {
		pt.With().Info("late lower rank detected",
			log.String("id_malicious", msg.PubKey.String()))
		pt.isConflicting = true
	}
}

// IsConflicting returns true if there was a conflict, false otherwise.
func (pt *proposalTracker) IsConflicting() bool {
	return pt.isConflicting
}

// ProposedSet returns the proposed set if there is a valid proposal, nil otherwise.
func (pt *proposalTracker) ProposedSet() *Set {
	if pt.proposal == nil {
		return nil
	}

	if pt.isConflicting {
		return nil
	}

	return NewSet(pt.proposal.InnerMsg.Values)
}
