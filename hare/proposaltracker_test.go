package hare

import (
	"context"
	"log"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func buildProposalMsg(signing Signer, s *Set, signature []byte) *Msg {
	builder := newMessageBuilder().SetRoleProof(signature)
	builder.SetType(proposal).SetInstanceID(instanceID1).SetRoundCounter(proposalRound).SetKi(ki).SetValues(s).SetSVP(buildSVP(ki, NewSetFromValues(value1)))
	builder.SetEligibilityCount(1)
	builder = builder.SetPubKey(signing.PublicKey()).Sign(signing)

	return builder.Build()
}

func BuildProposalMsg(signing Signer, s *Set) *Msg {
	return buildProposalMsg(signing, s, []byte{})
}

func TestProposalTracker_OnProposalConflict(t *testing.T) {
	log.Fatal()

	s := NewSetFromValues(value1, value2)
	verifier := signing.NewEdSigner([20]byte{})

	m1 := BuildProposalMsg(verifier, s)
	tracker := newProposalTracker(logtest.New(t))
	tracker.OnProposal(context.TODO(), m1)
	assert.False(t, tracker.IsConflicting())

	g := NewSetFromValues(value3)
	m2 := BuildProposalMsg(verifier, g)

	tracker.OnProposal(context.TODO(), m2)
	assert.True(t, tracker.IsConflicting())
}

func TestProposalTracker_IsConflicting(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	tracker := newProposalTracker(logtest.New(t))

	for i := 0; i < lowThresh10; i++ {
		tracker.OnProposal(context.TODO(), BuildProposalMsg(signing.NewEdSigner([20]byte{}), s))
		assert.False(t, tracker.IsConflicting())
	}
}

func TestProposalTracker_OnLateProposal(t *testing.T) {
	s := NewSetFromValues(value1, value2)
	verifier := signing.NewEdSigner([20]byte{})
	m1 := BuildProposalMsg(verifier, s)
	tracker := newProposalTracker(logtest.New(t))
	tracker.OnProposal(context.TODO(), m1)
	assert.False(t, tracker.IsConflicting())
	g := NewSetFromValues(value3)
	m2 := BuildProposalMsg(verifier, g)
	tracker.OnLateProposal(context.TODO(), m2)
	assert.True(t, tracker.IsConflicting())
}

func TestProposalTracker_ProposedSet(t *testing.T) {
	tracker := newProposalTracker(logtest.New(t))
	proposedSet := tracker.ProposedSet()
	assert.Nil(t, proposedSet)
	s1 := NewSetFromValues(value1, value2)
	tracker.OnProposal(context.TODO(), buildProposalMsg(signing.NewEdSigner([20]byte{}), s1, []byte{1, 2, 3}))
	proposedSet = tracker.ProposedSet()
	assert.NotNil(t, proposedSet)
	assert.True(t, s1.Equals(proposedSet))
	s2 := NewSetFromValues(value3, value4, value5)
	pub := signing.NewEdSigner([20]byte{})
	tracker.OnProposal(context.TODO(), buildProposalMsg(pub, s2, []byte{0}))
	proposedSet = tracker.ProposedSet()
	assert.True(t, s2.Equals(proposedSet))
	tracker.OnProposal(context.TODO(), buildProposalMsg(pub, s1, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0}))
	proposedSet = tracker.ProposedSet()
	assert.Nil(t, proposedSet)
}
