package server

import (
	"context"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

const protocol = "/protocol/test/1.0/"

func TestProtocol_SendRequest(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	fnd1 := NewMsgServer(context.TODO(), n1, protocol, 5*time.Second, make(chan service.DirectMessage, config.Values.BufferSize), log.NewDefault("t1"))

	//handler that returns some bytes on request

	handler := func(ctx context.Context, msg []byte) []byte {
		return []byte("some value to return")
	}
	// todo test nonbyte handlers
	fnd1.RegisterBytesMsgHandler(1, handler)

	n2 := sim.NewNode()
	fnd2 := NewMsgServer(context.TODO(), n2, protocol, 5*time.Second, make(chan service.DirectMessage, config.Values.BufferSize), log.NewDefault("t2"))

	//send request with handler that converts to string and sends via channel
	strCh := make(chan string)
	callback := func(msg []byte) {
		fmt.Println("callback ...")
		strCh <- string(msg)
	}

	err := fnd2.SendRequest(context.TODO(), 1, nil, n1.PublicKey(), callback, func(err error) {})
	msg := <-strCh

	assert.EqualValues(t, "some value to return", msg, "value received did not match correct value")
	assert.NoError(t, err, "Should not return error")

	// Now try sending to a bad address
	randkey := p2pcrypto.NewRandomPubkey()
	err = fnd2.SendRequest(context.TODO(), 1, nil, randkey, callback, func(err error) {})
	assert.Error(t, err, "Sending to bad address should return error")
}

func TestProtocol_CleanOldPendingMessages(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	fnd1 := NewMsgServer(context.TODO(), n1, protocol, 5*time.Second, make(chan service.DirectMessage, config.Values.BufferSize), log.NewDefault("t3"))

	//handler that returns some bytes on request

	handler := func(ctx context.Context, msg []byte) []byte {
		time.Sleep(2 * time.Second)
		return nil
	}

	fnd1.RegisterBytesMsgHandler(1, handler)

	n2 := sim.NewNode()
	fnd2 := NewMsgServer(context.TODO(), n2, protocol, 10*time.Millisecond, make(chan service.DirectMessage, config.Values.BufferSize), log.NewDefault("t4"))

	//send request with handler that converts to string and sends via channel
	strCh := make(chan string)
	callback := func(msg []byte) {
		fmt.Println("callback ...")
		strCh <- string(msg)
	}

	err := fnd2.SendRequest(context.TODO(), 1, nil, n1.PublicKey(), callback, func(err error) {})
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

func TestProtocol_Close(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	fnd1 := NewMsgServer(context.TODO(), n1, protocol, 5*time.Second, make(chan service.DirectMessage, config.Values.BufferSize), log.NewDefault("t5"))

	//handler that returns some bytes on request

	handler := func(ctx context.Context, msg []byte) []byte {
		time.Sleep(60 * time.Second)
		return nil
	}

	fnd1.RegisterBytesMsgHandler(1, handler)

	n2 := sim.NewNode()
	fnd2 := NewMsgServer(context.TODO(), n2, protocol, 10*time.Millisecond, make(chan service.DirectMessage, config.Values.BufferSize), log.NewDefault("t6"))

	//send request with handler that converts to string and sends via channel
	strCh := make(chan string)
	callback := func(msg []byte) {
		fmt.Println("callback ...")
		strCh <- string(msg)
	}

	err := fnd2.SendRequest(context.TODO(), 1, nil, n1.PublicKey(), callback, func(err error) {})
	assert.NoError(t, err, "Should not return error")
	assert.EqualValues(t, 1, fnd2.pendingQueue.Len(), "value received did not match correct value1")
	fnd2.Close()
}
