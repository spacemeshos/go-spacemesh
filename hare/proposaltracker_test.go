package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func BuildProposalMsg(pubKey crypto.PublicKey, s *Set) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(Proposal).SetInstanceId(*instanceId1).SetRoundCounter(Round2).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(pubKey).Sign(NewMockSigning())

	return builder.Build()
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