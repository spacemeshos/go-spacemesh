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

func BuildNotifyMsg(signing Signer, s *Set) *Msg {
	builder := newMessageBuilder()
	builder.SetType(notify).SetLayer(instanceID1).SetRoundCounter(notifyRound).SetCommittedRound(ki).SetValues(s)
	cert := &Certificate{}
	cert.Values = NewSetFromValues(value1).ToSlice()
	cert.AggMsgs = &AggregatedMessages{}
	cert.AggMsgs.Messages = []Message{BuildCommitMsg(signing, s).Message}
	builder.SetCertificate(cert)
	builder.SetEligibilityCount(1)
	return builder.SetPubKey(signing.PublicKey()).Sign(signing).Build()
}

func TestNotifyTracker_OnNotify(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)
	signer, err := signing.NewEdSigner()
	assert.NoError(t, err)

	mch := make(chan types.MalfeasanceGossip, lowDefaultSize)
	tracker := newNotifyTracker(logtest.New(t), mch, lowDefaultSize)
	m1 := BuildNotifyMsg(signer, s)
	exist := tracker.OnNotify(context.Background(), m1)
	assert.Equal(t, 1, tracker.NotificationsCount(s))
	assert.False(t, exist)
	exist = tracker.OnNotify(context.Background(), m1)
	assert.True(t, exist)
	require.Empty(t, mch)
	assert.Equal(t, 1, tracker.NotificationsCount(s))

	s.Add(value3)
	m2 := BuildNotifyMsg(signer, s)
	tracker.OnNotify(context.Background(), m2)
	assert.Equal(t, 0, tracker.NotificationsCount(s))
	require.Len(t, mch, 1)
	expected := types.MalfeasanceGossip{
		MalfeasanceProof: types.MalfeasanceProof{
			Layer: m1.Layer,
			Proof: types.Proof{
				Type: types.HareEquivocation,
				Data: &types.HareProof{
					Messages: [2]types.HareProofMsg{
						{
							InnerMsg:  m1.HareMetadata,
							Signature: m1.Signature,
						},
						{
							InnerMsg:  m2.HareMetadata,
							Signature: m2.Signature,
						},
					},
				},
			},
		},
		Eligibility: &types.HareEligibilityGossip{
			Layer:       m2.Layer,
			Round:       m2.Round,
			PubKey:      m2.PubKey.Bytes(),
			Eligibility: m2.Eligibility,
		},
	}
	gossip := <-mch
	require.Equal(t, expected, gossip)
}

func TestNotifyTracker_NotificationsCount(t *testing.T) {
	signer1, err := signing.NewEdSigner()
	assert.NoError(t, err)

	signer2, err := signing.NewEdSigner()
	assert.NoError(t, err)

	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	mch := make(chan types.MalfeasanceGossip, lowDefaultSize)
	tracker := newNotifyTracker(logtest.New(t), mch, lowDefaultSize)
	tracker.OnNotify(context.Background(), BuildNotifyMsg(signer1, s))
	assert.Equal(t, 1, tracker.NotificationsCount(s))
	tracker.OnNotify(context.Background(), BuildNotifyMsg(signer2, s))
	assert.Equal(t, 2, tracker.NotificationsCount(s))
	require.Empty(t, mch)
}
