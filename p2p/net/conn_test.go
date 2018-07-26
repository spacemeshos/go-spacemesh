package net

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/delimited"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"net"
	"testing"
	"time"
)

func generatePublicKey() crypto.PublicKey {
	_, pubKey, _ := crypto.GenerateKeyPair()
	return pubKey
}

func generateIpAddress() string {
	return fmt.Sprintf("%d.%d.%d.%d", rand.Int31n(255), rand.Int31n(255), rand.Int31n(255), rand.Int31n(255))
}

func TestSendReceiveMessage(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := generatePublicKey()
	formatter := delimited.NewChan(10)
	conn := newConnection(rwcam, netw, formatter, rPub, netw.logger)
	conn.SetSession(&NetworkSessionImpl{})
	go conn.beginEventProcessing()
	msg := "hello"
	err := conn.Send([]byte(msg))
	assert.NoError(t, err)
	assert.Equal(t, len(msg)+1, len(rwcam.WriteOut())) // the +1 is because of the delimited wire format
	rwcam.SetReadResult(rwcam.WriteOut(), nil)
	data := <-netw.IncomingMessages()
	assert.Equal(t, []byte(msg), data.Message)
}

func TestReceiveError(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := generatePublicKey()
	formatter := delimited.NewChan(10)
	conn := newConnection(rwcam, netw, formatter, rPub, netw.logger)
	conn.SetSession(&NetworkSessionImpl{})

	go conn.beginEventProcessing()
	rwcam.SetReadResult([]byte{}, fmt.Errorf("fail"))
	closedConn := <-netw.ClosingConnections()
	assert.Equal(t, conn.id, closedConn.ID())
}

func TestSendError(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := generatePublicKey()
	formatter := delimited.NewChan(10)
	conn := newConnection(rwcam, netw, formatter, rPub, netw.logger)
	conn.SetSession(&NetworkSessionImpl{})
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
	rPub := generatePublicKey()
	formatter := delimited.NewChan(10)
	conn := newConnection(rwcam, netw, formatter, rPub, netw.logger)
	go conn.beginEventProcessing()
	rwcam.SetReadResult([]byte{3, 1, 1, 1}, nil)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), netw.PreSessionCount())
}

func TestPreSessionError(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := generatePublicKey()
	formatter := delimited.NewChan(10)
	conn := newConnection(rwcam, netw, formatter, rPub, netw.logger)
	netw.SetPreSessionResult(fmt.Errorf("fail"))
	go conn.beginEventProcessing()
	rwcam.SetReadResult([]byte{3, 1, 1, 1}, nil)
	closedConn := <-netw.ClosingConnections()
	assert.Equal(t, conn.id, closedConn.ID())
	assert.Equal(t, int32(1), netw.PreSessionCount())
}

func TestClose(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := generatePublicKey()
	formatter := delimited.NewChan(10)
	conn := newConnection(rwcam, netw, formatter, rPub, netw.logger)
	conn.SetSession(&NetworkSessionImpl{})
	go conn.beginEventProcessing()
	conn.Close()
	closedConn := <-netw.ClosingConnections()
	assert.Equal(t, 1, rwcam.CloseCount())
	assert.Equal(t, conn.id, closedConn.ID())
}

func TestDoubleClose(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	rPub := generatePublicKey()
	formatter := delimited.NewChan(10)
	conn := newConnection(rwcam, netw, formatter, rPub, netw.logger)
	conn.SetSession(&NetworkSessionImpl{})
	go conn.beginEventProcessing()
	conn.Close()
	closedConn := <-netw.ClosingConnections()
	assert.Equal(t, 1, rwcam.CloseCount())
	assert.Equal(t, conn.id, closedConn.ID())
	conn.Close()

	timer := time.NewTimer(100 * time.Millisecond)
	select {
	case <-netw.ClosingConnections():
		assert.True(t, false)
	case <-timer.C:
	}
}

func TestGettersToBoostCoverage(t *testing.T) {
	netw := NewNetworkMock()
	rwcam := NewReadWriteCloseAddresserMock()
	addr := net.TCPAddr{net.ParseIP("1.1.1.1"), 555, "ipv4"}
	rwcam.setRemoteAddrResult(&addr)
	rPub := generatePublicKey()
	formatter := delimited.NewChan(10)
	conn := newConnection(rwcam, netw, formatter, rPub, netw.logger)
	assert.Equal(t, 36, len(conn.ID()))
	assert.Equal(t, conn.ID(), conn.String())
	rPub = generatePublicKey()
	conn.SetRemotePublicKey(rPub)
	assert.Equal(t, rPub, conn.RemotePublicKey())
	conn.SetSession(&NetworkSessionImpl{})
	assert.NotNil(t, conn.Session())
	assert.NotNil(t, conn.incomingChannel())
	assert.Equal(t, addr.String(), conn.RemoteAddr().String())

}
