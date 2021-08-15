package server

import (
	"context"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const protocol = "/protocol/test/1.0/"

func TestProtocol_ResponseNoDataNoError(t *testing.T) {
	l := logtest.New(t).WithName("response_no_data_no_err")
	data := SerializeResponse(l, nil, nil)
	assert.Greater(t, len(data), 0)
	resp, err := DeserializeResponse(l, data)
	assert.NoError(t, err)
	assert.Nil(t, resp.GetData())
	assert.NoError(t, resp.GetError())
}

func TestProtocol_ResponseNoError(t *testing.T) {
	l := logtest.New(t).WithName("response_no_err")
	bts := []byte("Baa Ram Ewe")
	data := SerializeResponse(l, bts, nil)
	assert.Greater(t, len(data), 0)
	resp, err := DeserializeResponse(l, data)
	assert.NoError(t, err)
	assert.Equal(t, bts, resp.GetData())
	assert.Equal(t, nil, resp.GetError())
}

func TestProtocol_ResponseHasError(t *testing.T) {
	l := logtest.New(t).WithName("response_has_err")
	data := SerializeResponse(l, nil, ErrShuttingDown)
	assert.Greater(t, len(data), 0)
	resp, err := DeserializeResponse(l, data)
	assert.NoError(t, err)
	assert.Nil(t, resp.GetData())
	assert.Equal(t, ErrShuttingDown, resp.GetError())
}

func TestProtocol_SendRequest(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	serv1 := NewMsgServer(context.TODO(), n1, protocol, 5*time.Second, make(chan service.DirectMessage, config.Values.BufferSize), logtest.New(t).WithName("serv1"))
	t.Cleanup(func() {
		serv1.Close()
	})
	//handler that returns some bytes on request

	mockData := "some value to return"
	handler := func(ctx context.Context, msg []byte) []byte {
		return SerializeResponse(serv1.Log, []byte(mockData), nil)
	}
	// todo test nonbyte handlers
	serv1.RegisterBytesMsgHandler(1, handler)

	n2 := sim.NewNode()
	serv2 := NewMsgServer(context.TODO(), n2, protocol, 5*time.Second, make(chan service.DirectMessage, config.Values.BufferSize), logtest.New(t).WithName("serv2"))
	t.Cleanup(func() {
		serv2.Close()
	})
	//send request with handler that converts to string and sends via channel
	respCh := make(chan Response)
	callback := func(resp Response) {
		respCh <- resp
	}

	err := serv2.SendRequest(context.TODO(), 1, nil, n1.PublicKey(), callback, func(err error) {})
	require.NoError(t, err, "Should not return error")
	resp := <-respCh

	assert.EqualValues(t, mockData, resp.GetData(), "value received did not match correct value")
	assert.NoError(t, resp.GetError(), "should not receive error from peer")

	// Now try sending to a bad address
	randkey := p2pcrypto.NewRandomPubkey()
	err = serv2.SendRequest(context.TODO(), 1, nil, randkey, callback, func(err error) {})
	assert.Error(t, err, "Sending to bad address should return error")
}

func TestProtocol_SendRequestPeerReturnError(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	srv1 := NewMsgServer(context.TODO(), n1, protocol, 5*time.Second, make(chan service.DirectMessage, config.Values.BufferSize), logtest.New(t).WithName("serv1"))
	t.Cleanup(func() {
		srv1.Close()
	})

	// handler returns error
	handler := func(ctx context.Context, msg []byte) []byte {
		return SerializeResponse(srv1.Log, nil, ErrBadRequest)
	}
	srv1.RegisterBytesMsgHandler(1, handler)

	n2 := sim.NewNode()
	srv2 := NewMsgServer(context.TODO(), n2, protocol, 5*time.Second, make(chan service.DirectMessage, config.Values.BufferSize), logtest.New(t).WithName("serv2"))
	t.Cleanup(func() {
		srv2.Close()
	})
	respCh := make(chan Response)
	callback := func(resp Response) {
		respCh <- resp
	}

	err := srv2.SendRequest(context.TODO(), 1, nil, n1.PublicKey(), callback, func(err error) {})
	require.NoError(t, err)
	resp := <-respCh

	assert.Nil(t, resp.GetData(), "value received did not match correct value")
	assert.Equal(t, ErrBadRequest, resp.GetError(), "Should not return error")
}

func TestProtocol_CleanOldPendingMessages(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	serv1 := NewMsgServer(context.TODO(), n1, protocol, 5*time.Second, make(chan service.DirectMessage, config.Values.BufferSize), logtest.New(t).WithName("serv1"))
	t.Cleanup(func() {
		serv1.Close()
	})
	//handler that returns some bytes on request

	handler := func(ctx context.Context, msg []byte) []byte {
		time.Sleep(2 * time.Second)
		return nil
	}

	serv1.RegisterBytesMsgHandler(1, handler)

	n2 := sim.NewNode()
	serv2 := NewMsgServer(context.TODO(), n2, protocol, 10*time.Millisecond, make(chan service.DirectMessage, config.Values.BufferSize), logtest.New(t).WithName("serv2"))
	t.Cleanup(func() {
		serv2.Close()
	})
	//send request with handler that converts to string and sends via channel
	respCh := make(chan Response)
	callback := func(resp Response) {
		respCh <- resp
	}

	err := serv2.SendRequest(context.TODO(), 1, nil, n1.PublicKey(), callback, func(err error) {})
	assert.NoError(t, err, "Should not return error")
	assert.EqualValues(t, 1, serv2.pendingQueue.Len(), "value received did not match correct value1")

	timeout := time.After(3 * time.Second)
	// Keep trying until we're timed out or got a result or got an error

	select {
	// Got a timeout! fail with a timeout error
	case <-timeout:
		t.Error("timeout")
		return
	default:
		if serv2.pendingQueue.Len() == 0 {
			assert.EqualValues(t, 0, serv2.pendingQueue.Len(), "value received did not match correct value2")
		}
	}
}

func TestProtocol_Close(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	serv1 := NewMsgServer(context.TODO(), n1, protocol, 5*time.Second, make(chan service.DirectMessage, config.Values.BufferSize), logtest.New(t).WithName("srv1"))

	t.Cleanup(func() {
		serv1.Close()
	})
	//handler that returns some bytes on request

	handler := func(ctx context.Context, msg []byte) []byte {
		time.Sleep(60 * time.Second)
		return SerializeResponse(serv1.Log, nil, nil)
	}

	serv1.RegisterBytesMsgHandler(1, handler)

	n2 := sim.NewNode()
	serv2 := NewMsgServer(context.TODO(), n2, protocol, 10*time.Millisecond, make(chan service.DirectMessage, config.Values.BufferSize), logtest.New(t).WithName("srv2"))
	t.Cleanup(func() {
		serv2.Close()
	})

	respCh := make(chan Response)
	callback := func(resp Response) {
		respCh <- resp
	}

	err := serv2.SendRequest(context.TODO(), 1, nil, n1.PublicKey(), callback, func(err error) {})
	assert.NoError(t, err, "Should not return error")
	assert.EqualValues(t, 1, serv2.pendingQueue.Len(), "value received did not match correct value1")
}
