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
	conn, err := cPool.GetConnection(addr, remotePub)
	assert.Nil(t, err)
	assert.Equal(t, remotePub.String(), conn.RemotePublicKey().String())
	assert.Equal(t, int32(1), net.GetDialCount())
}


func TestGetConnectionWithConnection(t *testing.T) {
	net := net.NewNetworkMock()
	net.SetDialDelayMs(50)
	net.SetDialResult(nil)
	cPool := NewConnectionPool(net, generatePublicKey())
	remotePub := generatePublicKey()
	addr := "1.1.1.1"
	cPool.GetConnection(addr, remotePub)
	conn, err := cPool.GetConnection(addr, remotePub)
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
	conn, aErr := cPool.GetConnection(addr, remotePub)
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
	rConn := net.NewConnectionMock(remotePub, net.Remote)
	cPool.newConn <- rConn
	time.Sleep(50 * time.Millisecond)
	conn, err := cPool.GetConnection(addr, remotePub)
	assert.Equal(t, remotePub.String(), conn.RemotePublicKey().String())
	assert.Equal(t, rConn.ID(), conn.ID())
	assert.Nil(t, err)
	assert.Equal(t, int32(0), n.GetDialCount())
}

func TestRemoteConnectionWithConnection(t *testing.T) {
	n := net.NewNetworkMock()
	remotePub := generatePublicKey()
	addr :="1.1.1.1"
	n.SetDialDelayMs(50)
	n.SetDialResult(nil)

	cPool := NewConnectionPool(n, generatePublicKey())
	conn, err := cPool.GetConnection(addr, remotePub)
	assert.Equal(t, remotePub.String(), conn.RemotePublicKey().String())
	assert.Nil(t, err)
	rConn := net.NewConnectionMock(remotePub, net.Remote)
	cPool.newConn <- rConn
	time.Sleep(50 * time.Millisecond)
	conn2, err := cPool.GetConnection(addr, remotePub)
	assert.Equal(t, remotePub.String(), conn.RemotePublicKey().String())
	assert.NotEqual(t, rConn.ID(), conn.ID())
	assert.Equal(t, conn2.ID(), conn.ID())
	assert.Nil(t, err)
	assert.Equal(t, int32(1), n.GetDialCount())
	assert.True(t, rConn.Closed())
}

func TestRemoteConnectionDuringDial(t *testing.T) {
	n := net.NewNetworkMock()
	remotePub := generatePublicKey()
	addr :="1.1.1.1"
	n.SetDialDelayMs(100)
	n.SetDialResult(nil)

	cPool := NewConnectionPool(n, generatePublicKey())
	waitCh := make(chan net.Connectioner)
	go func(ch chan net.Connectioner) {
		conn, _ := cPool.GetConnection(addr, remotePub)
		ch <- conn
	}(waitCh)
	rConn := net.NewConnectionMock(remotePub, net.Remote)
	time.Sleep(20 * time.Millisecond)
	cPool.newConn <- rConn
	getConn := <- waitCh
	assert.Equal(t, remotePub.String(), getConn.RemotePublicKey().String())
	assert.Equal(t, rConn.ID(), getConn.ID())
	assert.Equal(t, int32(1), n.GetDialCount())
	assert.False(t, rConn.Closed())
}

func TestShutdown(t *testing.T) {
	n := net.NewNetworkMock()
	n.SetDialDelayMs(100)
	n.SetDialResult(nil)
	remotePub := generatePublicKey()
	addr :="1.1.1.1"

	cPool := NewConnectionPool(n, generatePublicKey())
	newConns := make(chan net.Connectioner)
	go func() {
		conn, _ := cPool.GetConnection(addr, remotePub)
		newConns <- conn
	}()
	time.Sleep(20 * time.Millisecond)
	cPool.Shutdown()
	conn := <- newConns
	cMock := conn.(*net.ConnectionMock)
	assert.True(t, cMock.Closed())
}

func TestGetConnectionAfterShutdown(t *testing.T) {
	n := net.NewNetworkMock()
	n.SetDialDelayMs(100)
	n.SetDialResult(nil)
	remotePub := generatePublicKey()
	addr :="1.1.1.1"

	cPool := NewConnectionPool(n, generatePublicKey())
	cPool.Shutdown()
	conn, err := cPool.GetConnection(addr, remotePub)
	assert.NotNil(t, err)
	assert.Nil(t, conn)
}
//
//func TestShutdownWithMultipleDials(t *testing.T) {
//	n := net.NewNetworkMock()
//	n.SetDialDelayMs(100)
//	n.SetDialResult(nil)
//
//	cPool := NewConnectionPool(n, generatePublicKey())
//	newConns := make(chan net.Connectioner)
//	iterCnt := 20
//	for i := 0;	i < iterCnt; i++ {
//		go func() {
//			addr := fmt.Sprintf("%d.%d.%d.%d", rand.Int31n(255), rand.Int31n(255), rand.Int31n(255), rand.Int31n(255))
//			key := generatePublicKey()
//			conn, _ := cPool.GetConnection(addr, key)
//			newConns <- conn
//		}()
//	}
//	time.Sleep(20 * time.Millisecond)
//	cPool.Shutdown()
//	var cnt int
//	for conn := range newConns {
//		cMock := conn.(*net.ConnectionMock)
//		assert.True(t, cMock.Closed())
//		cnt++
//		if cnt == iterCnt {
//			break
//		}
//	}
//}