package hare3

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGradedGossip(t *testing.T) {
	gg := NewDefaultGradedGossiper()

	// Good message
	res, euivocationHash := gg.ReceiveMsg(msgHash1, id1, values3, r0)
	assert.Equal(t, SendValue, res)
	assert.Nil(t, euivocationHash)

	// Good message for different round
	res, euivocationHash = gg.ReceiveMsg(msgHash1, id1, values3, r1)
	assert.Equal(t, SendValue, res)
	assert.Nil(t, euivocationHash)

	// Equivocation
	res, euivocationHash = gg.ReceiveMsg(msgHash1, id1, values3, r1)
	assert.Equal(t, SendEquivocationProof, res)
	assert.Equal(t, &msgHash1, euivocationHash)

	// Drop message
	res, euivocationHash = gg.ReceiveMsg(msgHash1, id1, values2, r1)
	assert.Equal(t, DropMessage, res)
	assert.Nil(t, euivocationHash)

	// Known malicious
	res, euivocationHash = gg.ReceiveMsg(msgHash1, id2, nil, r1)
	assert.Equal(t, SendValue, res)
	assert.Nil(t, euivocationHash)

	// Further message from previous equivocator now known malicious (drop message)
	res, euivocationHash = gg.ReceiveMsg(msgHash1, id1, nil, r1)
	assert.Equal(t, DropMessage, res)
	assert.Nil(t, euivocationHash)
}
