package hare

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func marshallUnmarshall(t *testing.T, msg *Message) *Message {
	buf, err := types.InterfaceToBytes(&msg)
	require.NoError(t, err)

	m := &Message{}
	require.NoError(t, types.BytesToInterface(buf, m))
	return m
}

func TestBuilder_TestBuild(t *testing.T) {
	b := newMessageBuilder()
	sgn := signing.NewEdSigner()
	msg := b.SetPubKey(sgn.PublicKey()).SetInstanceID(instanceID1).Sign(sgn).Build()

	m := marshallUnmarshall(t, msg.Message)
	assert.Equal(t, m, msg.Message)
}

func TestMessageBuilder_SetValues(t *testing.T) {
	s := NewSetFromValues(value5)
	msg := newMessageBuilder().SetValues(s).Build().Message

	m := marshallUnmarshall(t, msg)
	s1 := NewSet(m.InnerMsg.Values)
	s2 := NewSet(msg.InnerMsg.Values)
	assert.True(t, s1.Equals(s2))
}

func TestMessageBuilder_SetCertificate(t *testing.T) {
	s := NewSetFromValues(value5)
	tr := newCommitTracker(1, 1, s)
	tr.OnCommit(BuildCommitMsg(signing.NewEdSigner(), s))
	cert := tr.BuildCertificate()
	assert.NotNil(t, cert)
	c := newMessageBuilder().SetCertificate(cert).Build().Message
	cert2 := marshallUnmarshall(t, c).InnerMsg.Cert
	assert.Equal(t, cert.Values, cert2.Values)
}
