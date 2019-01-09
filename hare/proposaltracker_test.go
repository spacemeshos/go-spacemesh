package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func buildProposalMsg(pubKey crypto.PublicKey, s *Set, signature Signature) *pb.HareMessage {
	builder := NewMessageBuilder().SetRoleProof(signature)
	builder.SetType(Proposal).SetInstanceId(*instanceId1).SetRoundCounter(Round2).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(pubKey).Sign(NewMockSigning())

	return builder.Build()
}

func BuildProposalMsg(pubKey crypto.PublicKey, s *Set) *pb.HareMessage {
	return buildProposalMsg(pubKey, s, Signature{})
}

func TestProposalTracker_OnProposalConflict(t *testing.T) {
	s := NewSetFromValues(value1, value2)
	pubKey := generatePubKey(t)

	m1 := BuildProposalMsg(pubKey, s)
	tracker := NewProposalTracker(lowThresh10)
	tracker.OnProposal(m1)
	assert.False(t, tracker.IsConflicting())
	s.Add(value3)
	m2 := BuildProposalMsg(pubKey, s)
	tracker.OnProposal(m2)
	assert.True(t, tracker.IsConflicting())
}

func TestProposalTracker_IsConflicting(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	tracker := NewProposalTracker(lowThresh10)

	for i := 0; i < lowThresh10; i++ {
		tracker.OnProposal(BuildProposalMsg(generatePubKey(t), s))
		assert.False(t, tracker.IsConflicting())
	}
}

func TestProposalTracker_OnLateProposal(t *testing.T) {
	s := NewSetFromValues(value1, value2)
	pubKey := generatePubKey(t)
	m1 := BuildProposalMsg(pubKey, s)
	tracker := NewProposalTracker(lowThresh10)
	tracker.OnProposal(m1)
	assert.False(t, tracker.IsConflicting())
	s.Add(value3)
	m2 := BuildProposalMsg(pubKey, s)
	tracker.OnLateProposal(m2)
	assert.True(t, tracker.IsConflicting())
}

func TestProposalTracker_ProposedSet(t *testing.T) {
	tracker := NewProposalTracker(lowThresh10)
	proposedSet := tracker.ProposedSet()
	assert.Nil(t, proposedSet)
	s1 := NewSetFromValues(value1, value2)
	tracker.OnProposal(buildProposalMsg(generatePubKey(t), s1, Signature{1, 2, 3}))
	proposedSet = tracker.ProposedSet()
	assert.NotNil(t, proposedSet)
	assert.True(t, s1.Equals(proposedSet))
	s2 := NewSetFromValues(value3, value4, value5)
	pub := generatePubKey(t)
	tracker.OnProposal(buildProposalMsg(pub, s2, Signature{0}))
	proposedSet = tracker.ProposedSet()
	assert.True(t, s2.Equals(proposedSet))
	tracker.OnProposal(buildProposalMsg(pub, s1, Signature{0}))
	proposedSet = tracker.ProposedSet()
	assert.Nil(t, proposedSet)
}