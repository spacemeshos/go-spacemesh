package hare

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/signing"
)

func BuildCertifyMsg(signing Signer, s *Set) *Msg {
	builder := newMessageBuilder()
	builder.SetType(certify).SetInstanceID(instanceID1).SetRoundCounter(notifyRound).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(signing.PublicKey()).Sign(signing)
	cert := &certificate{}
	cert.Values = NewSetFromValues(value1).ToSlice()
	cert.AggMsgs = &aggregatedMessages{}
	cert.AggMsgs.Messages = []*Message{BuildCommitMsg(signing, s).Message}
	builder.SetCertificate(cert)
	builder.SetEligibilityCount(1)
	return builder.Build()
}

func TestCertifyTracker_OnCommit(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := newCertifyTracker(lowThresh10 + 1)

	for i := 0; i < lowThresh10; i++ {
		m := BuildCertifyMsg(signing.NewEdSigner(), s)
		certified := tracker.OnCertify(m)
		assert.False(t, certified)
	}

	assert.True(t, tracker.OnCertify(BuildCertifyMsg(signing.NewEdSigner(), s)))
}

func TestCertifyTracker_OnCommitDuplicate(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := newCertifyTracker(2)
	verifier := signing.NewEdSigner()
	assert.Equal(t, 0, len(tracker.certifiers))
	tracker.OnCertify(BuildCertifyMsg(verifier, s))
	assert.Equal(t, 1, len(tracker.certifiers))
	tracker.OnCertify(BuildCertifyMsg(verifier, s))
	assert.Equal(t, 1, len(tracker.certifiers))
	tracker.OnCertify(BuildCertifyMsg(signing.NewEdSigner(), s))
	assert.Equal(t, 2, len(tracker.certifiers))
}

func TestCertifyTracker_HasEnoughCommits(t *testing.T) {
	s := NewSetFromValues(value1)
	tracker := newCertifyTracker(2)
	assert.False(t, tracker.IsCertified())
	tracker.OnCertify(BuildCertifyMsg(signing.NewEdSigner(), s))
	assert.False(t, tracker.IsCertified())
	tracker.OnCertify(BuildCertifyMsg(signing.NewEdSigner(), s))
	assert.True(t, tracker.IsCertified())
}
