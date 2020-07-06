package hare

import (
	"bytes"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"testing"
)

func marshallUnmarshall(t *testing.T, msg *Message) *Message {
	var w bytes.Buffer
	_, err := xdr.Marshal(&w, &msg)
	if err != nil {
		assert.Fail(t, "Failed to marshal data")
	}

	m := &Message{}
	r := bytes.NewReader(w.Bytes())
	_, err = xdr.Unmarshal(r, m)
	if err != nil {
		assert.FailNow(t, "cant unmarshal")
	}

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
	tr := newCommitTracker(s, Committee{Weight: 1, Size: 1})
	tr.OnCommit(BuildCommitMsg(signing.NewEdSigner(), s))
	cert := tr.BuildCertificate()
	assert.NotNil(t, cert)
	c := newMessageBuilder().SetCertificate(cert).Build().Message
	cert2 := marshallUnmarshall(t, c).InnerMsg.Cert
	assert.Equal(t, cert.Values, cert2.Values)
}
