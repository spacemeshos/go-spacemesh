package hare

import (
	"bytes"
	"github.com/spacemeshos/go-spacemesh/log"
)

type proposalTracker interface {
	OnProposal(msg *Msg)
	OnLateProposal(msg *Msg)
	IsConflicting() bool
	ProposedSet() *Set
}

// Tracks proposal messages
type ProposalTracker struct {
	log.Log
	proposal      *Msg // maps PubKey->Proposal
	isConflicting bool // maps PubKey->ConflictStatus
}

func NewProposalTracker(log log.Log) *ProposalTracker {
	pt := &ProposalTracker{}
	pt.proposal = nil
	pt.isConflicting = false
	pt.Log = log

	return pt
}

// Tracks the provided proposal message assuming it was received on the expected round
func (pt *ProposalTracker) OnProposal(msg *Msg) {
	if pt.proposal == nil { // first leader
		pt.proposal = msg // just update
		return
	}

	// if same sender then we should check for equivocation
	if bytes.Equal(pt.proposal.PubKey, msg.PubKey) {
		s := NewSet(msg.Message.Values)
		g := NewSet(pt.proposal.Message.Values)
		if !s.Equals(g) { // equivocation detected
			pt.With().Info("Equivocation detected", log.String("id_malicious", string(msg.PubKey)),
				log.String("current_set", g.String()), log.String("conflicting_set", s.String()))
			pt.isConflicting = true
		}

		return // process done
	}

	// ignore msgs with higher ranked role proof
	if bytes.Compare(msg.Message.RoleProof, pt.proposal.Message.RoleProof) > 0 {
		return
	}

	pt.proposal = msg        // update lower leader msg
	pt.isConflicting = false // assume no conflict
}

// Tracks the provided proposal message assuming it was late
func (pt *ProposalTracker) OnLateProposal(msg *Msg) {
	if pt.proposal == nil {
		return
	}

	// if same sender then we should check for equivocation
	if bytes.Equal(pt.proposal.PubKey, msg.PubKey) {
		s := NewSet(msg.Message.Values)
		g := NewSet(pt.proposal.Message.Values)
		if !s.Equals(g) { // equivocation detected
			pt.With().Info("Equivocation detected", log.String("id_malicious", string(msg.PubKey)),
				log.String("current_set", g.String()), log.String("conflicting_set", s.String()))
			pt.isConflicting = true
		}
	}

	// not equal check rank
	// lower ranked proposal on late proposal is a conflict
	if bytes.Compare(msg.Message.RoleProof, pt.proposal.Message.RoleProof) < 0 {
		pt.With().Info("late lower rank detected", log.String("id_malicious", string(msg.PubKey)))
		pt.isConflicting = true
	}
}

// Checks if there was a conflict of proposals
// Returns true if there was a conflict, false otherwise
func (pt *ProposalTracker) IsConflicting() bool {
	return pt.isConflicting
}

// Returns the proposed set if valid, nil otherwise
func (pt *ProposalTracker) ProposedSet() *Set {
	if pt.proposal == nil {
		return nil
	}

	if pt.isConflicting {
		return nil
	}

	return NewSet(pt.proposal.Message.Values)
}
