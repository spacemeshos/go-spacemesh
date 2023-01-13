package hare

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func BuildCommitMsg(signing Signer, s *Set) *Msg {
	builder := newMessageBuilder()
	builder.SetType(commit).SetLayer(instanceID1).SetRoundCounter(commitRound).SetCommittedRound(ki).SetValues(s)
	builder.SetEligibilityCount(1)
	builder = builder.SetPubKey(signing.PublicKey()).Sign(signing)
	return builder.Build()
}

func TestCommitTracker_OnCommit(t *testing.T) {
	s := NewSetFromValues(value1)
	mch := make(chan types.MalfeasanceGossip, lowThresh10)
	tracker := newCommitTracker(logtest.New(t), mch, lowThresh10+1, lowThresh10, s)

	for i := 0; i < lowThresh10; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		m := BuildCommitMsg(signer, s)
		tracker.OnCommit(context.Background(), m)
		assert.False(t, tracker.HasEnoughCommits())
	}

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	tracker.OnCommit(context.Background(), BuildCommitMsg(signer, s))
	assert.True(t, tracker.HasEnoughCommits())
	require.Empty(t, mch)
}

func TestCommitTracker_OnCommitDuplicate(t *testing.T) {
	s := NewSetFromValues(value1)
	mch := make(chan types.MalfeasanceGossip, 2)
	tracker := newCommitTracker(logtest.New(t), mch, 2, 2, s)
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)
	assert.Equal(t, 0, len(tracker.seenSenders))
	tracker.OnCommit(context.Background(), BuildCommitMsg(signer1, s))
	assert.Equal(t, 1, len(tracker.seenSenders))
	assert.Equal(t, 1, len(tracker.commits))
	tracker.OnCommit(context.Background(), BuildCommitMsg(signer1, s))
	assert.Equal(t, 1, len(tracker.seenSenders))
	tracker.OnCommit(context.Background(), BuildCommitMsg(signer2, s))
	assert.Equal(t, 2, len(tracker.seenSenders))
	require.Empty(t, mch)
}

func TestCommitTracker_OnCommitEquivocate(t *testing.T) {
	s1 := NewSetFromValues(value1)
	s2 := NewSetFromValues(value2)
	mch := make(chan types.MalfeasanceGossip, 2)
	tracker := newCommitTracker(logtest.New(t), mch, 2, 2, s1)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	msg1 := BuildCommitMsg(signer, s1)
	msg2 := BuildCommitMsg(signer, s2)
	tracker.OnCommit(context.Background(), msg1)
	tracker.OnCommit(context.Background(), msg2)
	require.Len(t, mch, 1)
	expected := types.MalfeasanceGossip{
		MalfeasanceProof: types.MalfeasanceProof{
			Layer: msg1.Layer,
			Proof: types.Proof{
				Type: types.HareEquivocation,
				Data: &types.HareProof{
					Messages: [2]types.HareProofMsg{
						{
							InnerMsg:  msg1.HareMetadata,
							Signature: msg1.Signature,
						},
						{
							InnerMsg:  msg2.HareMetadata,
							Signature: msg2.Signature,
						},
					},
				},
			},
		},
		Eligibility: &types.HareEligibilityGossip{
			Layer:       msg2.Layer,
			Round:       msg2.Round,
			PubKey:      msg2.PubKey.Bytes(),
			Eligibility: msg2.Eligibility,
		},
	}
	gossip := <-mch
	require.Equal(t, expected, gossip)
}

func TestCommitTracker_HasEnoughCommits(t *testing.T) {
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)

	s := NewSetFromValues(value1)
	mch := make(chan types.MalfeasanceGossip, 2)
	tracker := newCommitTracker(logtest.New(t), mch, 2, 2, s)
	assert.False(t, tracker.HasEnoughCommits())
	tracker.OnCommit(context.Background(), BuildCommitMsg(signer1, s))
	tracker.OnCommit(context.Background(), BuildCommitMsg(signer2, s))
	assert.True(t, tracker.HasEnoughCommits())
	require.Empty(t, mch)
}

func TestCommitTracker_BuildCertificate(t *testing.T) {
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)

	s := NewSetFromValues(value1)
	tracker := newCommitTracker(logtest.New(t), make(chan types.MalfeasanceGossip, 2), 2, 2, s)
	assert.Nil(t, tracker.BuildCertificate())
	tracker.OnCommit(context.Background(), BuildCommitMsg(signer1, s))
	tracker.OnCommit(context.Background(), BuildCommitMsg(signer2, s))
	cert := tracker.BuildCertificate()
	assert.Equal(t, 2, len(cert.AggMsgs.Messages))
	assert.Nil(t, cert.AggMsgs.Messages[0].InnerMsg.Values)
	assert.Nil(t, cert.AggMsgs.Messages[1].InnerMsg.Values)
}
