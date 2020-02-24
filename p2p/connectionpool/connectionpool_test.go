package connectionpool

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	net2 "net"
	"testing"
	"time"
)

func generatePublicKey() p2pcrypto.PublicKey {
	return p2pcrypto.NewRandomPubkey()
}

func generateIpAddress() string {
	return fmt.Sprintf("%d.%d.%d.%d", rand.Int31n(255), rand.Int31n(255), rand.Int31n(255), rand.Int31n(255))
}

func TestGetConnectionWithNoConnection(t *testing.T) {
	n := net.NewNetworkMock()
	n.SetDialDelayMs(50)
	n.SetDialResult(nil)
	cPool := NewConnectionPool(n.Dial, generatePublicKey(), log.NewDefault(t.Name()))
	remotePub := generatePublicKey()
	addr := net2.TCPAddr{net2.ParseIP("1.1.1.1"), 0000, ""}
	conn, err := cPool.GetConnection(&addr, remotePub)
	assert.Nil(t, err)
	assert.Equal(t, remotePub.String(), conn.RemotePublicKey().String())
	assert.Equal(t, int32(1), n.DialCount())
}

func TestGetConnectionWithConnection(t *testing.T) {
	n := net.NewNetworkMock()
	n.SetDialDelayMs(50)
	n.SetDialResult(nil)
	cPool := NewConnectionPool(n.Dial, generatePublicKey(), log.NewDefault(t.Name()))
	remotePub := generatePublicKey()
	addr := net2.TCPAddr{net2.ParseIP("1.1.1.1"), 0000, ""}
	conn, err := cPool.GetConnection(&addr, remotePub)
	assert.Nil(t, err)
	assert.Equal(t, remotePub.String(), conn.RemotePublicKey().String())
	assert.Equal(t, int32(1), n.DialCount())
}

func TestGetConnectionWithError(t *testing.T) {
	n := net.NewNetworkMock()
	n.SetDialDelayMs(50)
	eErr := errors.New("err")
	n.SetDialResult(eErr)
	cPool := NewConnectionPool(n.Dial, generatePublicKey(), log.NewDefault(t.Name()))
	remotePub := generatePublicKey()
	addr := net2.TCPAddr{net2.ParseIP("1.1.1.1"), 0000, ""}
	conn, aErr := cPool.GetConnection(&addr, remotePub)
	assert.Equal(t, eErr, aErr)
	assert.Nil(t, conn)
	assert.Equal(t, int32(1), n.DialCount())
}

func TestGetConnectionDuringDial(t *testing.T) {
	n := net.NewNetworkMock()
	remotePub := generatePublicKey()
	addr := net2.TCPAddr{net2.ParseIP("1.1.1.1"), 0000, ""}
	n.SetDialDelayMs(100)
	n.SetDialResult(nil)

	cPool := NewConnectionPool(n.Dial, generatePublicKey(), log.NewDefault(t.Name()))
	waitCh := make(chan net.Connection)
	// dispatch 2 GetConnection calls
	dispatchF := func(ch chan net.Connection) {
		conn, _ := cPool.GetConnection(&addr, remotePub)
		assert.Equal(t, remotePub.String(), conn.RemotePublicKey().String())
		ch <- conn
	}
	go dispatchF(waitCh)
	go dispatchF(waitCh)
	cnt := 0
	var prevId string
Loop:
	for {
		select {
		case c := <-waitCh:
			if prevId == "" {
				prevId = c.ID()
			} else {
				assert.Equal(t, prevId, c.ID())
			}
			cnt++
			if cnt == 2 {
				break Loop
			}
		case <-time.After(120 * time.Millisecond):
			fmt.Println("timeout!")
			assert.True(t, false)
			break Loop
		}
	}
	assert.Equal(t, int32(1), n.DialCount())
}

func TestRemoteConnectionWithNoConnection(t *testing.T) {
	n := net.NewNetworkMock()
	remotePub := generatePublicKey()
	addr := net2.TCPAddr{net2.ParseIP("1.1.1.1"), 0000, ""}
	n.SetDialDelayMs(50)
	n.SetDialResult(nil)

	cPool := NewConnectionPool(n.Dial, generatePublicKey(), log.NewDefault(t.Name()))
	rConn := net.NewConnectionMock(remotePub)
	rConn.SetSession(net.NewSessionMock(remotePub))
	cPool.OnNewConnection(net.NewConnectionEvent{rConn, nil})
	time.Sleep(50 * time.Millisecond)
	conn, err := cPool.GetConnection(&addr, remotePub)
	assert.Equal(t, remotePub.String(), conn.RemotePublicKey().String())
	assert.Equal(t, rConn.ID(), conn.ID())
	assert.Nil(t, err)
	assert.Equal(t, int32(0), n.DialCount())
}

func TestRemoteConnectionWithExistingConnection(t *testing.T) {
	n := net.NewNetworkMock()
	addr := net2.TCPAddr{net2.ParseIP("1.1.1.1"), 0000, ""}
	cPool := NewConnectionPool(n.Dial, generatePublicKey(), log.NewDefault(t.Name()))

	lowPubkey, err := p2pcrypto.NewPublicKeyFromBase58("7gd5cD8ZanFaqnMHZrgUsUjDeVxMTxfpnu4gDPS69pBU")
	assert.NoError(t, err)
	highPubkey, err := p2pcrypto.NewPublicKeyFromBase58("FABBx9LKEo9dEpQeo6GBmygoqrrC34JnzDPtz1jL6qAA")
	assert.NoError(t, err)

	// local connection has session ID < remote's session ID
	remotePub := highPubkey
	localPub := lowPubkey

	localSession := net.NewSessionMock(localPub)
	n.SetNextDialSessionID(localSession.ID().Bytes())
	lConn, _ := cPool.GetConnection(&addr, remotePub)
	rConn := net.NewConnectionMock(remotePub)
	rConn.SetSession(net.NewSessionMock(remotePub))
	cPool.OnNewConnection(net.NewConnectionEvent{rConn, nil})
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, remotePub.String(), lConn.RemotePublicKey().String())
	assert.Equal(t, int32(1), n.DialCount())
	assert.False(t, rConn.Closed())
	assert.True(t, lConn.Closed())

	// local connection has session ID > remote's session ID
	remotePub = lowPubkey
	localPub = highPubkey

	localSession = net.NewSessionMock(localPub)
	n.SetNextDialSessionID(localSession.ID().Bytes())
	lConn, _ = cPool.GetConnection(&addr, remotePub)
	rConn = net.NewConnectionMock(remotePub)
	rConn.SetSession(net.NewSessionMock(remotePub))
	cPool.OnNewConnection(net.NewConnectionEvent{rConn, nil})
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, remotePub.String(), lConn.RemotePublicKey().String())
	assert.Equal(t, int32(2), n.DialCount())
	assert.True(t, rConn.Closed())
	assert.False(t, lConn.Closed())
}

func TestShutdown(t *testing.T) {
	n := net.NewNetworkMock()
	n.SetDialDelayMs(100)
	n.SetDialResult(nil)
	remotePub := generatePublicKey()
	addr := net2.TCPAddr{net2.ParseIP("1.1.1.1"), 0000, ""}

	cPool := NewConnectionPool(n.Dial, generatePublicKey(), log.NewDefault(t.Name()))
	newConns := make(chan net.Connection)
	go func() {
		conn, _ := cPool.GetConnection(&addr, remotePub)
		newConns <- conn
	}()
	time.Sleep(20 * time.Millisecond)
	cPool.Shutdown()
	conn := <-newConns
	cMock := conn.(*net.ConnectionMock)
	assert.True(t, cMock.Closed())
}

func TestGetConnectionAfterShutdown(t *testing.T) {
	n := net.NewNetworkMock()
	n.SetDialDelayMs(100)
	n.SetDialResult(nil)
	remotePub := generatePublicKey()
	addr := net2.TCPAddr{net2.ParseIP("1.1.1.1"), 0000, ""}

	cPool := NewConnectionPool(n.Dial, generatePublicKey(), log.NewDefault(t.Name()))
	cPool.Shutdown()
	conn, err := cPool.GetConnection(&addr, remotePub)
	assert.NotNil(t, err)
	assert.Nil(t, conn)
}

func TestShutdownWithMultipleDials(t *testing.T) {
	n := net.NewNetworkMock()
	n.SetDialDelayMs(100)
	n.SetDialResult(nil)

	cPool := NewConnectionPool(n.Dial, generatePublicKey(), log.NewDefault(t.Name()))
	newConns := make(chan net.Connection)
	iterCnt := 20
	for i := 0; i < iterCnt; i++ {
		go func() {
			addr := net2.TCPAddr{net2.ParseIP(generateIpAddress()), 0000, ""}
			key := generatePublicKey()
			conn, err := cPool.GetConnection(&addr, key)
			if err == nil {
				newConns <- conn
			}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	cPool.Shutdown()
	var cnt int
	for conn := range newConns {
		cMock := conn.(*net.ConnectionMock)
		assert.True(t, cMock.Closed(), "connection %s is still open", cMock.ID())
		cnt++
		if cnt == iterCnt {
			break
		}
	}
}

func TestClosedConnection(t *testing.T) {
	nMock := net.NewNetworkMock()
	nMock.SetDialDelayMs(50)
	nMock.SetDialResult(nil)
	cPool := NewConnectionPool(nMock.Dial, generatePublicKey(), log.NewDefault(t.Name()))
	remotePub := generatePublicKey()
	addr := net2.TCPAddr{net2.ParseIP("1.1.1.1"), 0000, ""}

	nMock.SubscribeClosingConnections(cPool.OnClosedConnection)
	// create connection
	conn, _ := cPool.GetConnection(&addr, remotePub)

	// report that the connection was closed
	nMock.PublishClosingConnection(net.ConnectionWithErr{conn, errors.New("testerr")})

	// query same connection and assert that it's a new instance
	conn2, _ := cPool.GetConnection(&addr, remotePub)

	assert.NotEqual(t, conn.ID(), conn2.ID())
	assert.Equal(t, int32(2), nMock.DialCount())
}

func TestRandom(t *testing.T) {
	type Peer struct {
		key  p2pcrypto.PublicKey
		addr string
	}

	peerCnt := 30
	peers := make([]Peer, 0)
	for i := 0; i < peerCnt; i++ {
		peers = append(peers, Peer{generatePublicKey(), generateIpAddress()})
	}

	nMock := net.NewNetworkMock()
	nMock.SetDialDelayMs(50)
	nMock.SetDialResult(nil)
	cPool := NewConnectionPool(nMock.Dial, generatePublicKey(), log.NewDefault(t.Name()))
	rand.Seed(time.Now().UnixNano())
	for {
		r := rand.Int31n(3)
		if r == 0 {
			go func() {
				peer := peers[rand.Int31n(int32(peerCnt))]
				rConn := net.NewConnectionMock(peer.key)
				sID := p2pcrypto.NewRandomPubkey()
				rConn.SetSession(net.NewSessionMock(sID))
				cPool.OnNewConnection(net.NewConnectionEvent{rConn, nil})
			}()
		} else if r == 1 {
			go func() {
				peer := peers[rand.Int31n(int32(peerCnt))]
				addr := net2.TCPAddr{net2.ParseIP(peer.addr), 0000, ""}
				conn, err := cPool.GetConnection(&addr, peer.key)
				assert.Nil(t, err)
				nMock.PublishClosingConnection(net.ConnectionWithErr{conn, errors.New("testerr")})
			}()
		} else {
			go func() {
				peer := peers[rand.Int31n(int32(peerCnt))]
				addr := net2.TCPAddr{net2.ParseIP(peer.addr), 0000, ""}
				_, err := cPool.GetConnection(&addr, peer.key)
				assert.Nil(t, err)
			}()
		}
		time.Sleep(10 * time.Millisecond)

		if rand.Int31n(100) == 0 {
			cPool.Shutdown()
			break
		}
	}
}

func TestConnectionPool_GetConnectionIfExists(t *testing.T) {
	n := net.NewNetworkMock()
	addr := "1.1.1.1"
	cPool := NewConnectionPool(n.Dial, generatePublicKey(), log.NewDefault(t.Name()))

	pk, err := p2pcrypto.NewPublicKeyFromBase58("7gd5cD8ZanFaqnMHZrgUsUjDeVxMTxfpnu4gDPS69pBU")
	assert.NoError(t, err)

	conn := net.NewConnectionMock(pk)
	conn.SetSession(net.NewSessionMock(p2pcrypto.NewRandomPubkey()))

	nd := node.NewNode(pk, net2.ParseIP(addr), 1010, 1010)

	cPool.OnNewConnection(net.NewConnectionEvent{conn, nd})

	getcon, err := cPool.GetConnectionIfExists(pk)

	require.NoError(t, err)
	require.Equal(t, getcon, conn)
	require.Equal(t, int(n.DialCount()), 0)
}

func TestConnectionPool_GetConnectionIfExists_Concurrency(t *testing.T) {
	n := net.NewNetworkMock()
	addr := "1.1.1.1"
	cPool := NewConnectionPool(n.Dial, generatePublicKey(), log.NewDefault(t.Name()))

	pk, err := p2pcrypto.NewPublicKeyFromBase58("7gd5cD8ZanFaqnMHZrgUsUjDeVxMTxfpnu4gDPS69pBU")
	assert.NoError(t, err)

	conn := net.NewConnectionMock(pk)
	conn.SetSession(net.NewSessionMock(p2pcrypto.NewRandomPubkey()))

	nd := node.NewNode(pk, net2.ParseIP(addr), 1010, 1010)

	cPool.OnNewConnection(net.NewConnectionEvent{conn, nd})

	i := 10
	done := make(chan struct{}, i)

	for j := 0; j < i; j++ {

		go func() {
			getcon, err := cPool.GetConnectionIfExists(pk)
			require.NoError(t, err)
			require.Equal(t, getcon, conn)
			require.Equal(t, int(n.DialCount()), 0)
			done <- struct{}{}
		}()

	}

	for ; i > 0; i-- {
		<-done
	}

}

func TestConnectionPool_CloseConnection(t *testing.T) {
	n := net.NewNetworkMock()
	addr := "1.1.1.1"
	cPool := NewConnectionPool(n.Dial, generatePublicKey(), log.NewDefault(t.Name()))

	pk, err := p2pcrypto.NewPublicKeyFromBase58("7gd5cD8ZanFaqnMHZrgUsUjDeVxMTxfpnu4gDPS69pBU")
	assert.NoError(t, err)

	conn := net.NewConnectionMock(pk)
	conn.SetSession(net.NewSessionMock(p2pcrypto.NewRandomPubkey()))

	nd := node.NewNode(pk, net2.ParseIP(addr), 1010, 1010)

	err = cPool.OnNewConnection(net.NewConnectionEvent{conn, nd})
	assert.NoError(t, err)

	cPool.CloseConnection(nd.PublicKey())
	_, found := cPool.connections[nd.PublicKey()]
	assert.False(t, found)
}
