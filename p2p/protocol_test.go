package p2p

import (
	"github.com/spacemeshos/go-spacemesh/p2p/simulator"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

const protocol = "/protocol/test/1.0/"

func TestProtocol_SendRequest(t *testing.T) {
	sim := simulator.New()
	n1 := sim.NewNode()
	fnd1 := NewProtocol(n1, protocol)

	//handler that returns some bytes on request
	handler := func(msg []byte) []byte { return []byte("some value to return") }
	fnd1.RegisterMsgHandler("msgType1", handler)

	n2 := sim.NewNode()
	fnd2 := NewProtocol(n2, protocol)

	//send request recive interface{} and verify
	b, err := fnd2.SendRequest("msgType1", nil, n1.PublicKey().String(), time.Minute)
	assert.NoError(t, err, "Should not return error")
	assert.EqualValues(t, []byte("some value to return"), b, "value received did not match correct value")
}

func TestProtocol_SendAsyncRequestRequest(t *testing.T) {

	sim := simulator.New()
	n1 := sim.NewNode()
	fnd1 := NewProtocol(n1, protocol)

	//handler that returns some bytes on request
	handler := func(msg []byte) []byte { return []byte("some value to return") }
	fnd1.RegisterMsgHandler("msgType1", handler)

	n2 := sim.NewNode()
	fnd2 := NewProtocol(n2, protocol)

	//send request with handler that converts to string
	b, err := fnd2.SendAsyncRequest("msgType1", nil, n1.PublicKey().String(), time.Minute, func(msg []byte) interface{} { return string(msg) })
	assert.NoError(t, err, "Should not return error")
	assert.EqualValues(t, "some value to return", b, "value received did not match correct value")
}
