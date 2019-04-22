package hare

import (
	"bytes"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"testing"
)

func marshallUnmarshall(t *testing.T, msg *XDRMessage) *XDRMessage {
	var w bytes.Buffer
	_, err := xdr.Marshal(&w, &msg)
	if err != nil {
		assert.Fail(t, "Failed to marshal data")
	}

	m := &XDRMessage{}
	r := bytes.NewReader(w.Bytes())
	_, err = xdr.Unmarshal(r, m)
	if err != nil {
		assert.FailNow(t, "cant unmarshal")
	}

	return m
}

func TestBuilder_TestBuild(t *testing.T) {
	b := NewMessageBuilder()
	sgn := signing.NewEdSigner()
	msg := b.SetPubKey(sgn.PublicKey().Bytes()).SetInstanceId(instanceId1).Sign(sgn).Build()

	m := marshallUnmarshall(t, msg.XDRMessage)
	assert.Equal(t, m, msg.XDRMessage)
}

func TestMessageBuilder_SetValues(t *testing.T) {
	s := NewSetFromValues(value5)
	msg := NewMessageBuilder().SetValues(s).Build().XDRMessage

	m := marshallUnmarshall(t, msg)
	s1 := NewSet(m.Message.Values)
	s2 := NewSet(msg.Message.Values)
	assert.True(t, s1.Equals(s2))
}

func TestMessageBuilder_SetCertificate(t *testing.T) {
	s := NewSetFromValues(value5)
	tr := NewCommitTracker(1, 1, s)
	tr.OnCommit(BuildCommitMsg(signing.NewEdSigner(), s))
	cert := tr.BuildCertificate()
	assert.NotNil(t, cert)
	c := NewMessageBuilder().SetCertificate(cert).Build().XDRMessage
	cert2 := marshallUnmarshall(t, c).Message.Cert
	assert.Equal(t, cert.Values, cert2.Values)
}
