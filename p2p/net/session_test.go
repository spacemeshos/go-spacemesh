package net

import (
	"testing"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/stretchr/testify/assert"
)

func TestSignature(t *testing.T) {
	priv1, pub1, _ := crypto.GenerateKeyPair()
	priv2, pub2, _ := crypto.GenerateKeyPair()
	var netId int8 = 1
	var port uint16 = 999

	hsMsg, session1, err1 := GenerateHandshakeRequestData(pub1, priv1, pub2, netId, port)
	assert.NoError(t, err1)
	_, session2, err2 := ProcessHandshakeRequest(netId, pub2, priv2, pub1, hsMsg)
	assert.NoError(t, err2)

	msg := "hello world"
	msgBytes := []byte(msg)
	sign, err3 := session1.Sign(msgBytes)
	assert.NoError(t, err3)

	assert.True(t, session2.VerifySignature(msgBytes, sign))
	assert.False(t, session2.VerifySignature(msgBytes, []byte("bla bla")))

	msg = "new message"
	msgBytes = []byte(msg)
	sign, err3 = session2.Sign(msgBytes)
	assert.NoError(t, err3)

	assert.True(t, session1.VerifySignature(msgBytes, sign))
	assert.False(t, session1.VerifySignature(msgBytes, []byte("bla bla")))
}
