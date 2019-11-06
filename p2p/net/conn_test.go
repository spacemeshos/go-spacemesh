package net

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

var msgSizeLimit = config.DefaultConfig().P2P.MsgSizeLimit

func TestSendReceiveMessage(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, netw.logger)
	go conn.beginEventProcessing()
	msg := "hello"
	err := conn.Send([]byte(msg))
	assert.NoError(t, err)
	assert.Equal(t, len(msg)+1, len(rwcam.WriteOut())) // the +1 is because of the delimited wire format
	rwcam.SetReadResult(rwcam.WriteOut(), nil)
	data := <-netw.IncomingMessages()[0]
	assert.Equal(t, []byte(msg), data.Message)
}

func TestReceiveError(t *testing.T) {
	runtime.GOMAXPROCS(1)
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, netw.logger)

	var wg sync.WaitGroup
	wg.Add(1)
	netw.SubscribeClosingConnections(func(closedConn ConnectionWithErr) {
		assert.Equal(t, conn.id, closedConn.Conn.ID())
		wg.Done()
	})

	go conn.beginEventProcessing()

	rwcam.SetReadResult([]byte{}, fmt.Errorf("fail"))
	wg.Wait()

}

func TestSendError(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, netw.logger)
	go conn.beginEventProcessing()

	rwcam.SetWriteResult(fmt.Errorf("fail"))
	msg := "hello"
	err := conn.Send([]byte(msg))
	assert.Error(t, err)
	assert.Equal(t, "fail", err.Error())
}

func TestPreSessionMessage(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, nil, msgSizeLimit, time.Second, netw.logger)
	rwcam.SetReadResult([]byte{3, 1, 1, 1}, nil)
	err := conn.setupIncoming(time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, rwcam.CloseCount(), 0)
	require.Equal(t, int32(1), netw.PreSessionCount())
}

func TestPreSessionMessageAfterSession(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, nil, msgSizeLimit, time.Second, netw.logger)
	rwcam.SetReadResult([]byte{3, 1, 1, 1}, nil)
	go conn.beginEventProcessing()
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, rwcam.CloseCount(), 1)
}

func TestConn_Limit(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, nil, 1, time.Second, netw.logger)
	rwcam.SetReadResult([]byte{5, 1, 2, 3, 4, 5, 6}, nil)
	err := conn.setupIncoming(time.Second)
	assert.EqualError(t, err, ErrMsgExceededLimit.Error())
}

func TestPreSessionError(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, nil, msgSizeLimit, time.Second, netw.logger)
	netw.SetPreSessionResult(fmt.Errorf("fail"))
	rwcam.SetReadResult(append([]byte{1}, []byte("0")...), nil)
	err := conn.setupIncoming(time.Second)
	require.Error(t, err)
}

func TestErrClose(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, netw.logger)

	var wg sync.WaitGroup
	wg.Add(1)
	netw.SubscribeClosingConnections(func(closedConn ConnectionWithErr) {
		assert.Equal(t, conn.id, closedConn.Conn.ID())
		wg.Done()
	})

	go conn.beginEventProcessing()
	rwcam.SetReadResult(nil, errors.New("not working"))
	wg.Wait()
	assert.Equal(t, 1, rwcam.CloseCount())
}
func TestClose(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, netw.logger)
	c := make(chan struct{}, 1)
	netw.SubscribeClosingConnections(func(connection ConnectionWithErr) {
		c <- struct{}{}
	})

	rwcam.SetWriteResult(errors.New("x"))
	rwcam.SetReadResult(nil, errors.New("x"))
	go conn.beginEventProcessing()
	//conn.Close()
	//err := conn.Send([]byte{3, 1,2,3})
	//assert.NoError(t, err)
	time.Sleep(time.Millisecond * 1000) // block a little bit
	assert.Equal(t, 1, rwcam.CloseCount())
	<-c

}

func TestDoubleClose(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, msgSizeLimit, time.Second, netw.logger)

	go conn.beginEventProcessing()
	conn.Close()
	time.Sleep(time.Millisecond * 10)
	assert.Equal(t, 1, rwcam.CloseCount())
	err := conn.Close()
	assert.Equal(t, ErrAlreadyClosed, err)
	assert.Equal(t, 1, rwcam.closeCnt)

	time.Sleep(100 * time.Millisecond) // just check that we don't panic
}

func TestGettersToBoostCoverage(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	addr := net.TCPAddr{net.ParseIP("1.1.1.1"), 555, "ipv4"}
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
