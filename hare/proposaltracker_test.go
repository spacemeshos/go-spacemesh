package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func BuildProposalMsg(t *testing.T, pubKey crypto.PublicKey, s *Set) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(Proposal).SetLayer(*Layer1).SetIteration(k).SetKi(ki).SetBlocks(s)
	builder, err := builder.SetPubKey(pubKey).Sign(NewMockSigning())
	assert.Nil(t, err)

	return builder.Build()
}

func TestProposalTracker_OnProposalConflict(t *testing.T) {
	s := NewEmptySet()
	s.Add(blockId1)
	s.Add(blockId2)
	pubKey := generatePubKey(t)

	m1 := BuildProposalMsg(t, pubKey, s)
	tracker := NewProposalTracker()
	tracker.OnProposal(m1)
	assert.False(t, tracker.IsConflicting())
	s.Add(blockId3)
	m2 := BuildProposalMsg(t, pubKey, s)
	tracker.OnProposal(m2)
	assert.True(t, tracker.IsConflicting())
}

func TestProposalTracker_IsConflicting(t *testing.T) {
	s := NewEmptySet()
	s.Add(blockId1)
	tracker := NewProposalTracker()

	for i:=0;i<lowThresh10;i++ {
		tracker.OnProposal(BuildProposalMsg(t, generatePubKey(t), s))
		assert.False(t, tracker.IsConflicting())
	}
}