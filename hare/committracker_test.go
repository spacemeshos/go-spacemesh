package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func BuildCommitMsg(pubKey crypto.PublicKey, s *Set) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(Commit).SetLayer(*setId1).SetIteration(k).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(pubKey).Sign(NewMockSigning())

	return builder.Build()
}

func TestCommitTracker_OnCommit(t *testing.T) {
	tracker := NewCommitTracker(lowThresh10+1, lowThresh10)
	assert.Equal(t, 0, tracker.getMaxCommits())
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	m := BuildCommitMsg(generatePubKey(t), s)
	tracker.OnCommit(m)
	assert.Equal(t, 1, tracker.getMaxCommits())
	s.Add(value2)
	for i := 0; i < lowThresh10; i++ {
		m := BuildCommitMsg(generatePubKey(t), s)
		tracker.OnCommit(m)
	}
	assert.Equal(t, lowThresh10, tracker.getMaxCommits())

	assert.False(t, tracker.HasEnoughCommits())
	tracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	assert.Equal(t, lowThresh10+1, tracker.getMaxCommits())
	assert.True(t, tracker.HasEnoughCommits())
}
