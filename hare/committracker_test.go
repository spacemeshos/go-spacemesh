package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func BuildCommitMsg(t *testing.T, pubKey crypto.PublicKey, s *Set) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(Commit).SetLayer(*Layer1).SetIteration(k).SetKi(ki).SetBlocks(s)
	builder, err := builder.SetPubKey(pubKey).Sign(NewMockSigning())
	assert.Nil(t, err)

	return builder.Build()
}

func TestCommitTracker_OnCommit(t *testing.T) {
	tracker := NewCommitTracker(lowThresh10 + 1)
	assert.Equal(t, 0, tracker.getMaxCommits())
	s := NewEmptySet()
	s.Add(blockId1)
	m := BuildCommitMsg(t, generatePubKey(t), s)
	tracker.OnCommit(m)
	assert.Equal(t, 1, tracker.getMaxCommits())
	s.Add(blockId2)
	for i := 0; i < lowThresh10; i++ {
		m := BuildCommitMsg(t, generatePubKey(t), s)
		tracker.OnCommit(m)
	}
	assert.Equal(t, lowThresh10, tracker.getMaxCommits())

	assert.False(t, tracker.HasEnoughCommits())
	tracker.OnCommit(BuildCommitMsg(t, generatePubKey(t), s))
	assert.Equal(t, lowThresh10+1, tracker.getMaxCommits())
	assert.True(t, tracker.HasEnoughCommits())
}
