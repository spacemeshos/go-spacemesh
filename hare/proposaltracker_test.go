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

func BuildProposalMsgWithRoleProof(pubKey crypto.PublicKey, s *Set, signature Signature) *pb.HareMessage {
	msg := BuildProposalMsg(pubKey, s)
	msg.Message.RoleProof = signature

	return msg
}

func TestProposalTracker_OnProposalConflict(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)
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

func TestProposalTracker_OnProposal_IgnoreMsgWithLowerRankedRoleProof(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)

	pubKey := generatePubKey(t)
	m1 := BuildProposalMsgWithRoleProof(pubKey, s, []byte{1})

	tracker := NewProposalTracker(lowThresh10)
	tracker.OnProposal(m1)

	s.Add(value2)
	newPubKey := generatePubKey(t)
	m2 := BuildProposalMsgWithRoleProof(newPubKey, s, []byte{2})
	tracker.OnProposal(m2)
	assert.NotEqual(t, tracker.proposal, m2)
}

func TestProposalTracker_OnLateProposal(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)

	pubKey := generatePubKey(t)
	m1 := BuildProposalMsg(pubKey, s)

	tracker := NewProposalTracker(lowThresh10)
	tracker.OnProposal(m1)

	s.Add(value2)
	m2 := BuildProposalMsg(pubKey, s)
	tracker.OnLateProposal(m2)
	assert.True(t, tracker.IsConflicting())
}

func TestProposalTracker_TestProposalTracker_OnLateProposal_IgnoreMsgWithHigherRankedRoleProof(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)

	pubKey := generatePubKey(t)
	m1 := BuildProposalMsgWithRoleProof(pubKey, s, []byte{2})

	tracker := NewProposalTracker(lowThresh10)
	tracker.OnProposal(m1)

	s.Add(value2)
	newPubKey := generatePubKey(t)
	m2 := BuildProposalMsgWithRoleProof(newPubKey, s, []byte{1})
	tracker.OnLateProposal(m2)
	assert.NotEqual(t, tracker.proposal, m2)
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

func TestProposalTracker_ProposedSet(t *testing.T) {
	tracker := NewProposalTracker(lowThresh10)
	expectNil := tracker.ProposedSet()
	assert.Nil(t, expectNil)

	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)
	pubKey := generatePubKey(t)

	m1 := BuildProposalMsg(pubKey, s)
	tracker.OnProposal(m1)
	proposedSet := tracker.ProposedSet()
	assert.True(t, proposedSet.Equals(s));
	s.Add(value3)
	m2 := BuildProposalMsg(pubKey, s)
	tracker.OnProposal(m2) // isConflicting will be true
	proposedSetWhenConflicting := tracker.ProposedSet()
	assert.Nil(t, proposedSetWhenConflicting)
}
