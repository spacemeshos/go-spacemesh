package connectionpool

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/stretchr/testify/assert"
	"testing"
	"errors"
	"time"
)

func generatePublicKey() crypto.PublicKey {
	_, pubKey, _ := crypto.GenerateKeyPair()
	return pubKey
}

func TestGetConnectionWithNoConnection(t *testing.T) {
	net := net.NewNetworkMock()
	net.SetDialDelayMs(50)
	net.SetDialResult(nil)
	cPool := NewConnectionPool(net, generatePublicKey())
	remotePub := generatePublicKey()
	addr := "1.1.1.1"
	conn, err := cPool.getConnection(addr, remotePub)
	assert.Nil(t, err)
	assert.Equal(t, remotePub.String(), conn.RemotePublicKey().String())
	assert.Equal(t, 1, net.GetDialCount())
}


func TestGetConnectionWithConnection(t *testing.T) {
	net := net.NewNetworkMock()
	net.SetDialDelayMs(50)
	net.SetDialResult(nil)
	cPool := NewConnectionPool(net, generatePublicKey())
	remotePub := generatePublicKey()
	addr := "1.1.1.1"
	cPool.getConnection(addr, remotePub)
	conn, err := cPool.getConnection(addr, remotePub)
	assert.Nil(t, err)
	assert.Equal(t, remotePub.String(), conn.RemotePublicKey().String())
	assert.Equal(t, int32(1), net.GetDialCount())
}

func TestGetConnectionWithError(t *testing.T) {
	net := net.NewNetworkMock()
	net.SetDialDelayMs(50)
	eErr := errors.New("err")
	net.SetDialResult(eErr)
	cPool := NewConnectionPool(net, generatePublicKey())
	remotePub := generatePublicKey()
	addr := "1.1.1.1"
	conn, aErr := cPool.getConnection(addr, remotePub)
	assert.Equal(t, eErr, aErr)
	assert.Nil(t, conn)
	assert.Equal(t, int32(1), net.GetDialCount())
}

func TestRemoteConnectionWithNoConnection(t *testing.T) {
	n := net.NewNetworkMock()
	remotePub := generatePublicKey()
	addr := "1.1.1.1"
	n.SetDialDelayMs(50)
	n.SetDialResult(nil)

	cPool := NewConnectionPool(n, generatePublicKey())
	rConn := net.NewConnection(net.ReadWriteCloserMock{}, n, net.Remote, remotePub, n.GetLogger())
	cPool.newConn <- rConn
	time.Sleep(50 * time.Millisecond)conn, err := cPool.GetConnection(addr, remotePub)
	assert.Equal(t, remotePub.String(), conn.RemotePublicKey().String())
	assert.Equal(t, rConn.ID(), conn.ID())
	assert.Nil(t, err)
	assert.Equal(t, int32(0), n.GetDialCount())
}

func TestRemoteConnectionWithConnection(t *testing.T) {
	n := net.NewNetworkMock()
	remotePub := generatePublicKey()
	addr :="1.1.1.1"n.SetDialDelayMs(50)
	n.SetDialResult(nil)

	cPool := NewConnectionPool(n, generatePublicKey())
	conn, err := cPool.getConnection(addr, remotePub)
	assert.Equal(t, remotePub.String(), conn.RemotePublicKey().String())assert.Nil(t, err)
	rConn := net.NewConnection(net.ReadWriteCloserMock{}, n, net.Remote, remotePub, n.GetLogger())
	cPool.newConn <- rConn
	time.Sleep(50 * time.Millisecond)
	conn2, err := cPool.getConnection(addr, remotePub)
	assert.Equal(t, remotePub.String(), conn.RemotePublicKey().String())
	assert.NotEqual(t, rConn.ID(), conn.ID())
	assert.Equal(t, conn2.ID(), conn.ID())
	assert.Nil(t, err)
	assert.Equal(t, int32(1), n.GetDialCount())
}
