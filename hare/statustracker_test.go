package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func BuildStatusMsg(pubKey crypto.PublicKey, s *Set) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(Status).SetInstanceId(*instanceId1).SetRoundCounter(Round1).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(pubKey).Sign(NewMockSigning())

	return builder.Build()
}

func TestStatusTracker_RecordStatus(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)

	tracker := NewStatusTracker(lowThresh10+1, lowThresh10)
	assert.False(t, tracker.IsSVPReady())

	for i := 0; i < lowThresh10; i++ {
		tracker.RecordStatus(BuildPreRoundMsg(generatePubKey(t), s))
		assert.False(t, tracker.IsSVPReady())
	}

	tracker.RecordStatus(BuildPreRoundMsg(generatePubKey(t), s))
	assert.True(t, tracker.IsSVPReady())

	preStatusLen := len(tracker.statuses)
	tracker.RecordStatus(BuildPreRoundMsg(generatePubKey(t), s))
	assert.Equal(t, preStatusLen, len(tracker.statuses))
}

func TestStatusTracker_RecordStatus_NoSamePubKey(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)

	tracker := NewStatusTracker(lowThresh10, lowThresh10)
	msg := BuildPreRoundMsg(generatePubKey(t), s)

	tracker.RecordStatus(msg)
	preStatusLen := len(tracker.statuses)
	tracker.RecordStatus(msg)
	assert.Equal(t, preStatusLen, len(tracker.statuses))
}

func TestStatusTracker_RecordStatus_PubkeyError(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	msg := BuildPreRoundMsg(generatePubKey(t), s)
	msg.PubKey = []byte("wrong pub key")

	tracker := NewStatusTracker(lowThresh10, lowThresh10)
	tracker.RecordStatus(msg)

	assert.Equal(t, len(tracker.statuses), 0)
}

func TestStatusTracker_ProposalSet(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)

	tracker := NewStatusTracker(lowThresh10, lowThresh10)
	tracker.RecordStatus(BuildPreRoundMsg(generatePubKey(t), s))

	proposalSet := tracker.ProposalSet(lowThresh10)
	assert.Equal(t, proposalSet, tracker.buildUnionSet(lowThresh10))

	tracker.maxKi = 1
	assert.NotNil(t, tracker.ProposalSet(lowThresh10))

	tracker.maxRawSet = nil
	assert.Panics(t, func() {
		tracker.ProposalSet(lowThresh10)
	})
}

func TestStatusTracker_BuildSVP(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)

	tracker := NewStatusTracker(lowThresh10, lowThresh10)
	tracker.RecordStatus(BuildStatusMsg(generatePubKey(t), s))

	expectNil := tracker.BuildSVP()
	assert.Nil(t, expectNil)

	for i := 0; i < lowThresh10-1; i++ {
		tracker.RecordStatus(BuildPreRoundMsg(generatePubKey(t), s))
	}

	expectNotNil := tracker.BuildSVP()
	assert.NotNil(t, expectNotNil)
}

func TestStatusTracker_BuildUnionSet(t *testing.T) {
	tracker := NewStatusTracker(lowThresh10, lowThresh10)

	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	tracker.RecordStatus(BuildStatusMsg(generatePubKey(t), s))
	s.Add(value2)
	tracker.RecordStatus(BuildStatusMsg(generatePubKey(t), s))
	s.Add(value3)
	tracker.RecordStatus(BuildStatusMsg(generatePubKey(t), s))

	g := tracker.buildUnionSet(cfg.SetSize)
	assert.True(t, s.Equals(g))
}
