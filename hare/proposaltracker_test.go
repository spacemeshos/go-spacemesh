package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func buildProposalMsg(verifier Verifier, s *Set, signature Signature) *pb.HareMessage {
	builder := NewMessageBuilder().SetRoleProof(signature)
	builder.SetType(Proposal).SetInstanceId(*instanceId1).SetRoundCounter(Round2).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(verifier.Bytes()).Sign(NewMockSigning())

	return builder.Build()
}

func BuildProposalMsg(verifier Verifier, s *Set) *pb.HareMessage {
	return buildProposalMsg(verifier, s, Signature{})
}

func TestProposalTracker_OnProposalConflict(t *testing.T) {
	s := NewSetFromValues(value1, value2)
	verifier := generateVerifier(t)

	m1 := BuildProposalMsg(verifier, s)
	tracker := NewProposalTracker(lowThresh10)
	tracker.OnProposal(m1)
	assert.False(t, tracker.IsConflicting())
	s.Add(value3)
	m2 := BuildProposalMsg(verifier, s)
	tracker.OnProposal(m2)
	assert.True(t, tracker.IsConflicting())
}

func TestProposalTracker_IsConflicting(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	tracker := NewProposalTracker(lowThresh10)

	for i := 0; i < lowThresh10; i++ {
		tracker.OnProposal(BuildProposalMsg(generateVerifier(t), s))
		assert.False(t, tracker.IsConflicting())
	}
}

func TestProposalTracker_OnLateProposal(t *testing.T) {
	s := NewSetFromValues(value1, value2)
	verifier := generateVerifier(t)
	m1 := BuildProposalMsg(verifier, s)
	tracker := NewProposalTracker(lowThresh10)
	tracker.OnProposal(m1)
	assert.False(t, tracker.IsConflicting())
	s.Add(value3)
	m2 := BuildProposalMsg(verifier, s)
	tracker.OnLateProposal(m2)
	assert.True(t, tracker.IsConflicting())
}

func TestProposalTracker_ProposedSet(t *testing.T) {
	tracker := NewProposalTracker(lowThresh10)
	proposedSet := tracker.ProposedSet()
	assert.Nil(t, proposedSet)
	s1 := NewSetFromValues(value1, value2)
	tracker.OnProposal(buildProposalMsg(generateVerifier(t), s1, Signature{1, 2, 3}))
	proposedSet = tracker.ProposedSet()
	assert.NotNil(t, proposedSet)
	assert.True(t, s1.Equals(proposedSet))
	s2 := NewSetFromValues(value3, value4, value5)
	pub := generateVerifier(t)
	tracker.OnProposal(buildProposalMsg(pub, s2, Signature{0}))
	proposedSet = tracker.ProposedSet()
	assert.True(t, s2.Equals(proposedSet))
	tracker.OnProposal(buildProposalMsg(pub, s1, Signature{0}))
	proposedSet = tracker.ProposedSet()
	assert.Nil(t, proposedSet)
}
