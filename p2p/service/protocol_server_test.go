package service

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/simulator"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

const protocol = "/protocol/test/1.0/"

func TestProtocol_SendRequest(t *testing.T) {
	sim := simulator.New()
	n1 := sim.NewNode()
	fnd1 := NewProtocol(n1, protocol, 5*time.Second)

	//handler that returns some bytes on request
	handler := func(msg []byte) []byte { return []byte("some value to return") }
	fnd1.RegisterMsgHandler(1, handler)

	n2 := sim.NewNode()
	fnd2 := NewProtocol(n2, protocol, 5*time.Second)

	//send request recive interface{} and verify
	b, err := fnd2.SendRequest(1, nil, n1.PublicKey().String(), time.Minute)

	assert.NoError(t, err, "Should not return error")
	assert.EqualValues(t, []byte("some value to return"), b, "value received did not match correct value")
}

func TestProtocol_SendAsyncRequestRequest(t *testing.T) {
	sim := simulator.New()
	n1 := sim.NewNode()
	fnd1 := NewProtocol(n1, protocol, 5*time.Second)

	//handler that returns some bytes on request

	handler := func(msg []byte) []byte {
		return []byte("some value to return")
	}

	fnd1.RegisterMsgHandler(1, handler)

	n2 := sim.NewNode()
	fnd2 := NewProtocol(n2, protocol, 5*time.Second)

	//send request with handler that converts to string and sends via channel
	strCh := make(chan string)
	callback := func(msg []byte) {
		fmt.Println("callback ...")
		strCh <- string(msg)
	}

	err := fnd2.SendAsyncRequest(1, nil, n1.PublicKey().String(), callback)
	msg := <-strCh

	assert.EqualValues(t, "some value to return", string(msg), "value received did not match correct value")
	assert.NoError(t, err, "Should not return error")
}

func TestProtocol_CleanOldPendingMessages(t *testing.T) {
	sim := simulator.New()
	n1 := sim.NewNode()
	fnd1 := NewProtocol(n1, protocol, 5*time.Second)

	//handler that returns some bytes on request

	handler := func(msg []byte) []byte {
		time.Sleep(2 * time.Second)
		return nil
	}

	fnd1.RegisterMsgHandler(1, handler)

	n2 := sim.NewNode()
	fnd2 := NewProtocol(n2, protocol, 10*time.Millisecond)

	//send request with handler that converts to string and sends via channel
	strCh := make(chan string)
	callback := func(msg []byte) {
		fmt.Println("callback ...")
		strCh <- string(msg)
	}

	err := fnd2.SendAsyncRequest(1, nil, n1.PublicKey().String(), callback)
	assert.NoError(t, err, "Should not return error")
	assert.EqualValues(t, 1, fnd2.pendingQueue.Len(), "value received did not match correct value1")

	timeout := time.After(3 * time.Second)
	// Keep trying until we're timed out or got a result or got an error

	select {
	// Got a timeout! fail with a timeout error
	case <-timeout:
		t.Error("timeout")
		return
	default:
		if fnd2.pendingQueue.Len() == 0 {
			assert.EqualValues(t, 0, fnd2.pendingQueue.Len(), "value received did not match correct value2")
		}
	}

}
