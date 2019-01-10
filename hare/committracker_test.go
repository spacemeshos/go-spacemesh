package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func BuildCommitMsg(pubKey crypto.PublicKey, s *Set) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(Commit).SetInstanceId(*instanceId1).SetRoundCounter(Round3).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(pubKey).Sign(NewMockSigning())

	return builder.Build()
}

func TestCommitTracker_OnCommit(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := NewCommitTracker(lowThresh10+1, lowThresh10, s)

	for i := 0; i < lowThresh10; i++ {
		m := BuildCommitMsg(generatePubKey(t), s)
		tracker.OnCommit(m)
		assert.False(t, tracker.HasEnoughCommits())
	}

	s1 := NewSetFromValues(value2)
	m1 := BuildCommitMsg(generatePubKey(t), s1)
	tracker.OnCommit(m1)
	assert.Equal(t, len(tracker.commits), lowThresh10)

	m2 := BuildCommitMsg(generatePubKey(t), s)
	m2.PubKey = []byte("wrong pub key")
	tracker.OnCommit(m2)
	assert.Equal(t, len(tracker.commits), lowThresh10)

	tracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	assert.True(t, tracker.HasEnoughCommits())

	m3 := BuildCommitMsg(generatePubKey(t), s)
	tracker.OnCommit(m3)
	assert.Equal(t, len(tracker.commits), lowThresh10+1)
}

func TestCommitTracker_OnCommit_ProposedSetNil(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := NewCommitTracker(1, 1, s)
	tracker.proposedSet = nil

	m := BuildCommitMsg(generatePubKey(t), s)
	tracker.OnCommit(m)
	assert.Empty(t, tracker.commits)
}

func TestCommitTracker_OnCommitDuplicate(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := NewCommitTracker(2, 2, s)
	pub := generatePubKey(t)
	assert.Equal(t, 0, len(tracker.seenSenders))
	tracker.OnCommit(BuildCommitMsg(pub, s))
	assert.Equal(t, 1, len(tracker.seenSenders))
	assert.Equal(t, 1, len(tracker.commits))
	tracker.OnCommit(BuildCommitMsg(pub, s))
	assert.Equal(t, 1, len(tracker.seenSenders))
	tracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	assert.Equal(t, 2, len(tracker.seenSenders))
}

func TestCommitTracker_HasEnoughCommits(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := NewCommitTracker(2, 2, s)
	assert.False(t, tracker.HasEnoughCommits())
	tracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	tracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	assert.True(t, tracker.HasEnoughCommits())

	tracker.proposedSet = nil
	assert.False(t, tracker.HasEnoughCommits())
}

func TestCommitTracker_BuildCertificate(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := NewCommitTracker(2, 2, s)
	assert.Nil(t, tracker.BuildCertificate())
	tracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	tracker.OnCommit(BuildCommitMsg(generatePubKey(t), s))
	cert := tracker.BuildCertificate()
	assert.Equal(t, 2, len(cert.AggMsgs.Messages))
}
