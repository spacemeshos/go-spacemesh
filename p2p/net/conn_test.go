package net

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestSendReceiveMessage(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, netw.logger)
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
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, netw.logger)

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
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, netw.logger)
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
	conn := newConnection(rwcam, netw, rPub, nil, netw.logger)
	rwcam.SetReadResult([]byte{3, 1, 1, 1}, nil)
	err := conn.setupIncoming(time.Second)
	require.NoError(t, err)
	require.Equal(t, conn.Closed(), false)
	require.Equal(t, int32(1), netw.PreSessionCount())
}

func TestPreSessionMessageAfterSession(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, nil, netw.logger)
	rwcam.SetReadResult([]byte{3, 1, 1, 1}, nil)
	go conn.beginEventProcessing()
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, conn.Closed(), true)
}

func TestPreSessionError(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, nil, netw.logger)
	netw.SetPreSessionResult(fmt.Errorf("fail"))
	rwcam.SetReadResult(append([]byte{1}, []byte("0")...), nil)
	err := conn.setupIncoming(time.Second)
	require.Error(t, err)
}

func TestErrClose(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, netw.logger)

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
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, netw.logger)

	go conn.beginEventProcessing()
	conn.Close()
	time.Sleep(time.Millisecond * 1000) // block a little bit
	assert.Equal(t, 1, rwcam.CloseCount())
}

func TestDoubleClose(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, netw.logger)

	go conn.beginEventProcessing()
	conn.Close()
	time.Sleep(time.Millisecond * 10)
	assert.Equal(t, 1, rwcam.CloseCount())
	conn.Close()

	time.Sleep(100 * time.Millisecond) // just check that we don't panic
}

func TestGettersToBoostCoverage(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	addr := net.TCPAddr{net.ParseIP("1.1.1.1"), 555, "ipv4"}
	rwcam.setRemoteAddrResult(&addr)
	rPub := p2pcrypto.NewRandomPubkey()
	conn := newConnection(rwcam, netw, rPub, &networkSessionImpl{}, netw.logger)
	assert.Equal(t, 36, len(conn.ID()))
	assert.Equal(t, conn.ID(), conn.String())
	rPub = p2pcrypto.NewRandomPubkey()
	conn.SetRemotePublicKey(rPub)
	assert.Equal(t, rPub, conn.RemotePublicKey())
	assert.NotNil(t, conn.Session())
	assert.Equal(t, addr.String(), conn.RemoteAddr().String())

}
