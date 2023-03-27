package hare

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func marshallUnmarshall(t *testing.T, msg *Message) *Message {
	buf, err := codec.Encode(msg)
	require.NoError(t, err)

	m := &Message{}
	require.NoError(t, codec.Decode(buf, m))
	return m
}

func TestBuilder_TestBuild(t *testing.T) {
	b := newMessageBuilder()
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	msg := b.SetNodeID(signer.NodeID()).SetLayer(instanceID1).Sign(signer).Build()

	m := marshallUnmarshall(t, &msg.Message)
	assert.Equal(t, m, &msg.Message)
}

func TestMessageBuilder_SetValues(t *testing.T) {
	s := NewSetFromValues(types.ProposalID{5})
	msg := newMessageBuilder().SetValues(s).Build().Message

	m := marshallUnmarshall(t, &msg)
	s1 := NewSet(m.InnerMsg.Values)
	s2 := NewSet(msg.InnerMsg.Values)
	assert.True(t, s1.Equals(s2))
}

func TestMessageBuilder_SetCertificate(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	s := NewSetFromValues(types.ProposalID{5})
	et := NewEligibilityTracker(1)
	tr := newCommitTracker(logtest.New(t), commitRound, make(chan *types.MalfeasanceGossip), et, 1, 1, s)
	m := BuildCommitMsg(signer, s)
	et.Track(m.NodeID.Bytes(), m.Round, m.Eligibility.Count, true)
	tr.OnCommit(context.Background(), m)
	cert := tr.BuildCertificate()
	assert.NotNil(t, cert)
	c := newMessageBuilder().SetCertificate(cert).Build().Message
	cert2 := marshallUnmarshall(t, &c).InnerMsg.Cert
	assert.Equal(t, cert.Values, cert2.Values)
}

func TestMessageFromBuffer(t *testing.T) {
	b := newMessageBuilder()
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	msg := b.SetNodeID(signer.NodeID()).SetLayer(instanceID1).Sign(signer).Build().Message

	buf, err := codec.Encode(&msg)
	require.NoError(t, err)

	got, err := MessageFromBuffer(buf)
	require.NoError(t, err)
	require.Equal(t, msg, got)
}

func TestMessageFromBuffer_BadMsgHash(t *testing.T) {
	b := newMessageBuilder()
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	msg := b.SetNodeID(signer.NodeID()).SetLayer(instanceID1).Sign(signer).Build().Message
	msg.MsgHash = types.RandomHash()

	buf, err := codec.Encode(&msg)
	require.NoError(t, err)

	_, err = MessageFromBuffer(buf)
	require.Error(t, err)
}
