package net

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

var msgSizeLimit = config.DefaultTestConfig().P2P.MsgSizeLimit

func TestSendReceiveMessage(t *testing.T) {
	netw := NewNetworkMock(t)
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, netw.logger)
	go conn.beginEventProcessing(context.TODO())
	msg := "hello"
	err := conn.sendSock([]byte(msg))
	assert.NoError(t, err)
	assert.Equal(t, len(msg)+1, len(rwcam.WriteOut())) // the +1 is because of the delimited wire format
	rwcam.SetReadResult(rwcam.WriteOut(), nil)
	data := <-netw.IncomingMessages()[0]
	assert.Equal(t, []byte(msg), data.Message)
	err = conn.Close()
	assert.NoError(t, err)
}

func TestSendMessage(t *testing.T) {
	netw := NewNetworkMock(t)
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	rwcam.writeWaitChan = make(chan []byte)
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, netw.logger)
	go conn.beginEventProcessing(context.TODO())
	msg := "hello"
	err := conn.Send(context.TODO(), []byte(msg))
	assert.NoError(t, err)
	select {
	case <-rwcam.writeWaitChan:
		rcvmsg := <-rwcam.writeWaitChan
		assert.Equal(t, rcvmsg, []byte(msg))
	case <-time.After(time.Second):
		assert.Fail(t, "timeout waiting for message to be sent")
	}
}

func TestSendMutex(t *testing.T) {
	netw := NewNetworkMock(t)
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	rwcam.writeWaitChan = make(chan []byte) // unbuffered channel, write is blocking
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, netw.logger)
	go conn.beginEventProcessing(context.TODO())
	msg := "hello"

	// Send an initial message
	err := conn.Send(context.TODO(), []byte(msg))
	assert.NoError(t, err)

	// Make sure sendSock received the message and tried to send it (and is blocking on Write)
	timeout := time.After(1 * time.Second)
	for rwcam.WriteCount() < 1 {
		select {
		case <-timeout:
			assert.Fail(t, "timed out waiting for sendSock to call Write")
		case <-time.After(100 * time.Millisecond):
			continue
		}
	}

	// Now try sending another message and make sure it doesn't block
	done := make(chan struct{})
	go func() {
		err := conn.Send(context.TODO(), []byte(msg))
		assert.NoError(t, err)
		close(done)
	}()
	select {
	case <-done:
		// success
		break
	case <-time.After(time.Second):
		assert.Fail(t, "timed out waiting for message to be sent")
	}

	// make sure we can read both messages
	for i := 0; i < 2; i++ {
		select {
		// both messages should already be queued, so we should be able to read them immediately
		case <-rwcam.writeWaitChan:
			rcvmsg := <-rwcam.writeWaitChan
			assert.Equal(t, rcvmsg, []byte(msg))
		case <-time.After(time.Second):
			assert.Fail(t, "timed out waiting for message")
		}
	}
}

func TestReceiveError(t *testing.T) {
	runtime.GOMAXPROCS(1)
	netw := NewNetworkMock(t)
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, netw.logger)

	var wg sync.WaitGroup
	wg.Add(1)
	netw.SubscribeClosingConnections(func(ctx context.Context, closedConn ConnectionWithErr) {
		assert.Equal(t, conn.ID(), closedConn.Conn.ID())
		wg.Done()
	})

	go conn.beginEventProcessing(context.TODO())

	rwcam.SetReadResult([]byte{}, fmt.Errorf("fail"))
	wg.Wait()
}

func TestSendError(t *testing.T) {
	netw := NewNetworkMock(t)
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, netw.logger)
	go conn.beginEventProcessing(context.TODO())

	rwcam.SetWriteResult(fmt.Errorf("fail"))
	msg := "hello"
	err := conn.sendSock([]byte(msg))
	assert.Error(t, err)
	assert.Equal(t, "write record: write size: fail", err.Error())
}

func TestPreSessionMessage(t *testing.T) {
	netw := NewNetworkMock(t)
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, nil, msgSizeLimit, time.Second, netw.logger)
	rwcam.SetReadResult([]byte{3, 1, 1, 1}, nil)
	err := conn.setupIncoming(context.TODO(), time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, rwcam.CloseCount(), 0)
	require.Equal(t, int32(1), netw.PreSessionCount())
}

func TestPreSessionMessageAfterSession(t *testing.T) {
	netw := NewNetworkMock(t)
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, nil, msgSizeLimit, time.Second, netw.logger)
	rwcam.SetReadResult([]byte{3, 1, 1, 1}, nil)
	go conn.beginEventProcessing(context.TODO())
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, rwcam.CloseCount(), 1)
}

func TestConn_Limit(t *testing.T) {
	netw := NewNetworkMock(t)
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, nil, 1, time.Second, netw.logger)
	rwcam.SetReadResult([]byte{5, 1, 2, 3, 4, 5, 6}, nil)
	err := conn.setupIncoming(context.TODO(), time.Second)
	assert.EqualError(t, err, ErrMsgExceededLimit.Error())
}

func TestPreSessionError(t *testing.T) {
	netw := NewNetworkMock(t)
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, nil, msgSizeLimit, time.Second, netw.logger)
	netw.SetPreSessionResult(fmt.Errorf("fail"))
	rwcam.SetReadResult(append([]byte{1}, []byte("0")...), nil)
	err := conn.setupIncoming(context.TODO(), time.Second)
	require.Error(t, err)
}

func TestErrClose(t *testing.T) {
	netw := NewNetworkMock(t)
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, netw.logger)

	var wg sync.WaitGroup
	wg.Add(1)
	netw.SubscribeClosingConnections(func(ctx context.Context, closedConn ConnectionWithErr) {
		assert.Equal(t, conn.ID(), closedConn.Conn.ID())
		wg.Done()
	})

	go conn.beginEventProcessing(context.TODO())
	rwcam.SetReadResult(nil, errors.New("not working"))
	wg.Wait()
	assert.Equal(t, 1, rwcam.CloseCount())
}

func TestClose(t *testing.T) {
	netw := NewNetworkMock(t)
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, netw.logger)
	c := make(chan struct{}, 1)
	netw.SubscribeClosingConnections(func(ctx context.Context, connection ConnectionWithErr) {
		c <- struct{}{}
	})

	rwcam.SetWriteResult(errors.New("x"))
	rwcam.SetReadResult(nil, errors.New("x"))
	go conn.beginEventProcessing(context.TODO())
	time.Sleep(time.Millisecond * 1000) // block a little bit
	assert.Equal(t, 1, rwcam.CloseCount())
	<-c
}

func TestDoubleClose(t *testing.T) {
	netw := NewNetworkMock(t)
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, netw.logger)

	go conn.beginEventProcessing(context.TODO())
	assert.NoError(t, conn.Close())
	time.Sleep(time.Millisecond * 10)
	assert.Equal(t, 1, rwcam.CloseCount())
	err := conn.Close()
	assert.Equal(t, ErrAlreadyClosed, err)
	assert.Equal(t, 1, rwcam.closeCnt)

	time.Sleep(100 * time.Millisecond) // just check that we don't panic
}

func TestGettersToBoostCoverage(t *testing.T) {
	netw := NewNetworkMock(t)
	rwcam := NewReadWriteCloseAddresserMock()
	addr := net.TCPAddr{IP: net.ParseIP("1.1.1.1"), Port: 555}
	rwcam.setRemoteAddrResult(&addr)
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, netw.logger)
	assert.Equal(t, 36, len(conn.ID()))
	assert.Equal(t, conn.ID(), conn.String())
	rPub = p2pcrypto.NewRandomPubkey()
	conn.SetRemotePublicKey(rPub)
	assert.Equal(t, rPub, conn.RemotePublicKey())
	assert.NotNil(t, conn.Session())
	assert.Equal(t, addr.String(), conn.RemoteAddr().String())
}

func TestDropPeerWhenOutboundQueueFull(t *testing.T) {
	queueSize := 5
	rPub := p2pcrypto.NewRandomPubkey()

	runTest := func(t *testing.T, netw *NetworkMock, conn Connection, mChan chan msgToSend) {
		r := require.New(t)
		called := 0
		netw.SubscribeClosingConnections(func(_ context.Context, conn2 ConnectionWithErr) {
			r.Equal(conn, conn2.Conn)
			r.Equal(ErrQueueFull, conn2.Err)
			called++
		})
		msg := []byte("hello")

		// send enough messages to fill the channel
		for i := 0; i < queueSize; i++ {
			r.NoError(conn.Send(context.TODO(), msg))
		}
		r.Len(mChan, queueSize)
		r.Equal(0, called, "publishClosingConnection was called prematurely")

		// send one more message and expect conn not to block and peer to be dropped
		err := conn.Send(context.TODO(), msg)
		r.Equal(ErrQueueFull, err)
		r.Equal(1, called, "publishClosingConnection was not called")

		// expect connection to already be closed
		err = conn.Close()
		r.Equal(ErrAlreadyClosed, err)
	}

	t.Run("conn", func(t *testing.T) {
		netw := NewNetworkMock(t)
		rwcam := NewReadWriteCloseAddresserMock()
		rwcam.writeWaitChan = make(chan []byte)
		mChan := make(chan msgToSend, queueSize)
		conn := newConnectionWithMessagesChan(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, mChan, netw.logger)
		runTest(t, netw, conn, mChan)
	})

	t.Run("msgconn", func(t *testing.T) {
		netw := NewNetworkMock(t)
		rwcam := NewReadWriteCloseAddresserMock()
		rwcam.writeWaitChan = make(chan []byte)
		mChan := make(chan msgToSend, queueSize)
		conn := newMsgConnectionWithMessagesChan(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, mChan, netw.logger)
		runTest(t, netw, conn, mChan)
	})
}
