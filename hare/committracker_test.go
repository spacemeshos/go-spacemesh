package hare

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/signing"
)

func BuildCommitMsg(signing Signer, s *Set) *Msg {
	builder := newMessageBuilder()
	builder.SetType(commit).SetInstanceID(instanceID1).SetRoundCounter(commitRound).SetKi(ki).SetValues(s)
	builder.SetEligibilityCount(1)
	builder = builder.SetPubKey(signing.PublicKey()).Sign(signing)

	return builder.Build()
}

func TestCommitTracker_OnCommit(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := newCommitTracker(lowThresh10+1, lowThresh10, s)

	for i := 0; i < lowThresh10; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		m := BuildCommitMsg(signer, s)
		tracker.OnCommit(m)
		assert.False(t, tracker.HasEnoughCommits())
	}

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	tracker.OnCommit(BuildCommitMsg(signer, s))
	assert.True(t, tracker.HasEnoughCommits())
}

func TestCommitTracker_OnCommitDuplicate(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := newCommitTracker(2, 2, s)
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)
	assert.Equal(t, 0, len(tracker.seenSenders))
	tracker.OnCommit(BuildCommitMsg(signer1, s))
	assert.Equal(t, 1, len(tracker.seenSenders))
	assert.Equal(t, 1, len(tracker.commits))
	tracker.OnCommit(BuildCommitMsg(signer1, s))
	assert.Equal(t, 1, len(tracker.seenSenders))
	tracker.OnCommit(BuildCommitMsg(signer2, s))
	assert.Equal(t, 2, len(tracker.seenSenders))
}

func TestCommitTracker_HasEnoughCommits(t *testing.T) {
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)

	s := NewSetFromValues(value1)
	tracker := newCommitTracker(2, 2, s)
	assert.False(t, tracker.HasEnoughCommits())
	tracker.OnCommit(BuildCommitMsg(signer1, s))
	tracker.OnCommit(BuildCommitMsg(signer2, s))
	assert.True(t, tracker.HasEnoughCommits())
}

func TestCommitTracker_BuildCertificate(t *testing.T) {
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)
	
	s := NewSetFromValues(value1)
	tracker := newCommitTracker(2, 2, s)
	assert.Nil(t, tracker.BuildCertificate())
	tracker.OnCommit(BuildCommitMsg(signer1, s))
	tracker.OnCommit(BuildCommitMsg(signer2, s))
	cert := tracker.BuildCertificate()
	assert.Equal(t, 2, len(cert.AggMsgs.Messages))
	assert.Nil(t, cert.AggMsgs.Messages[0].InnerMsg.Values)
	assert.Nil(t, cert.AggMsgs.Messages[1].InnerMsg.Values)
}
