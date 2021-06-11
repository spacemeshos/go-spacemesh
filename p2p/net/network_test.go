package net

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func Test_sumByteArray(t *testing.T) {
	bytez := sumByteArray([]byte{0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1})
	assert.Equal(t, bytez, uint(20))
	bytez2 := sumByteArray([]byte{0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5, 0x5})
	assert.Equal(t, bytez2, uint(100))
}

func TestNet_EnqueueMessage(t *testing.T) {
	testnodes := 100
	cfg := config.DefaultConfig()
	ln, err := node.NewNodeIdentity()
	assert.NoError(t, err)
	n, err := NewNet(context.TODO(), cfg, ln, log.NewDefault(t.Name()))
	require.NoError(t, err)

	var rndmtx sync.Mutex
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))

	randmsg := func(b []byte) {
		rndmtx.Lock()
		rnd.Read(b)
		rndmtx.Unlock()
	}

	var timedout int32

	var wg sync.WaitGroup
	for i := 0; i < testnodes; i++ {
		wg.Add(1)
		go func() {
			rnode := node.GenerateRandomNodeData()
			sum := sumByteArray(rnode.PublicKey().Bytes())
			msg := make([]byte, 10)
			randmsg(msg)
			fmt.Printf("pushing %v to %v \r\n", hex.EncodeToString(msg), sum%n.queuesCount)
			n.EnqueueMessage(context.TODO(), IncomingMessageEvent{Conn: NewConnectionMock(rnode.PublicKey()), Message: msg})
			fmt.Printf("pushed %v to %v \r\n", hex.EncodeToString(msg), sum%n.queuesCount)
			tx := time.NewTimer(time.Second * 2)
			defer wg.Done()
			select {
			case <-n.IncomingMessages()[sum%n.queuesCount]:
				fmt.Printf("got %v \r\n", hex.EncodeToString(msg))
				//assert.Equal(t, s.Message, msg)
				//assert.Equal(t, s.Conn.RemotePublicKey(), rnode.PublicKey())
			case <-tx.C:
				fmt.Println("didn't get ", hex.EncodeToString(msg))
				atomic.AddInt32(&timedout, 1)
				return

			}
		}()
	}
	wg.Wait()
	if timedout > 0 {
		t.Fatal("timedout")
	}
	n.Shutdown()
}

type mockListener struct {
	calledCount    int32
	connReleaser   chan struct{}
	acceptResError error
	acceptFn       func() (net.Conn, error)
}

func newMockListener() *mockListener {
	return &mockListener{connReleaser: make(chan struct{})}
}

func (ml *mockListener) Accept() (net.Conn, error) {
	if ml.acceptFn != nil {
		return ml.acceptFn()
	}
	<-ml.connReleaser
	atomic.AddInt32(&ml.calledCount, 1)
	<-ml.connReleaser
	var c net.Conn = nil
	if ml.acceptResError == nil {
		c, _ = net.Pipe() // just for the interface lolz
	}
	return c, ml.acceptResError
}

func (ml *mockListener) releaseConn() {
	ml.connReleaser <- struct{}{}
	ml.connReleaser <- struct{}{}
}

func (ml *mockListener) Close() error {
	return nil
}
func (ml *mockListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Zone: ""}
}

type tempErr string

func (t tempErr) Error() string {
	return string(t)
}

func (t tempErr) Temporary() bool {
	return true
}

func Test_Net_LimitedConnections(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SessionTimeout = 100 * time.Millisecond

	ln, err := node.NewNodeIdentity()
	require.NoError(t, err)
	n, err := NewNet(context.TODO(), cfg, ln, log.NewDefault(t.Name()))
	//n.SubscribeOnNewRemoteConnections(counter)
	require.NoError(t, err)
	listener := newMockListener()
	n.Start(context.TODO(), listener)
	listener.acceptResError = tempErr("demo connection will close and allow more")
	for i := 0; i < cfg.MaxPendingConnections; i++ {
		listener.releaseConn()
	}

	require.Equal(t, atomic.LoadInt32(&listener.calledCount), int32(cfg.MaxPendingConnections))

	done := make(chan struct{})
	go func() {
		done <- struct{}{}
		listener.releaseConn()
		done <- struct{}{}
	}()
	require.Equal(t, atomic.LoadInt32(&listener.calledCount), int32(cfg.MaxPendingConnections))
	<-done
	<-done
	require.Equal(t, atomic.LoadInt32(&listener.calledCount), int32(cfg.MaxPendingConnections)+1)
	n.Shutdown()
}

func TestHandlePreSessionIncomingMessage2(t *testing.T) {
	r := require.New(t)
	var wg sync.WaitGroup

	aliceNode, aliceNodeInfo := node.GenerateTestNode(t)
	bobNode, _ := node.GenerateTestNode(t)

	bobsAliceConn := NewConnectionMock(aliceNode.PublicKey())
	bobsAliceConn.Addr = &net.TCPAddr{IP: aliceNodeInfo.IP, Port: int(aliceNodeInfo.ProtocolPort)}

	bobsNet, err := NewNet(context.TODO(), config.DefaultConfig(), bobNode, log.NewDefault(t.Name()))
	r.NoError(err)
	bobsNet.SubscribeOnNewRemoteConnections(func(event NewConnectionEvent) {
		r.Equal(aliceNode.PublicKey().String(), event.Conn.Session().ID().String(), "wrong session received")
		wg.Done()
	})

	aliceSessionWithBob := createSession(aliceNode.PrivateKey(), bobNode.PublicKey())
	aliceHandshakeMessageToBob, err := generateHandshakeMessage(aliceSessionWithBob, 1, 123, aliceNode.PublicKey())
	r.NoError(err)

	wg.Add(1)

	err = bobsNet.HandlePreSessionIncomingMessage(bobsAliceConn, aliceHandshakeMessageToBob)
	r.NoError(err)
	r.Equal(int32(0), bobsAliceConn.SendCount())

	wg.Wait()

	wg.Add(1)

	err = bobsNet.HandlePreSessionIncomingMessage(bobsAliceConn, aliceHandshakeMessageToBob)
	r.NoError(err)
	r.Equal(int32(0), bobsAliceConn.SendCount())

	wg.Wait()
	bobsNet.Shutdown()
}

func TestMaxPendingConnections(t *testing.T) {
	r := require.New(t)
	cfg := config.DefaultConfig()

	ln, err := node.NewNodeIdentity()
	require.NoError(t, err)
	n, err := NewNet(context.TODO(), cfg, ln, log.NewDefault(t.Name()))
	require.NoError(t, err)

	// Create many new connections
	pending := make(chan struct{})
	for i := 0; i < cfg.MaxPendingConnections-1; i++ {
		conn := NewConnectionMock(ln.PublicKey())
		go n.acceptAsync(context.TODO(), conn, pending)
	}

	// Now create one that we can shut down at will
	conn := NewConnectionMock(ln.PublicKey())
	wg := sync.WaitGroup{}
	wg.Add(1)
	conn.eventProcessing = func() {
		wg.Wait()
	}

	// this waitgroup makes sure that this goroutine terminates when we release the connection
	wgSuccess := sync.WaitGroup{}
	wgSuccess.Add(1)
	go func() {
		n.acceptAsync(context.TODO(), conn, pending)
		wgSuccess.Done()
	}()

	// Make sure that the listener does not accept a new connection until we terminate one
	counter := 0

	chDone := make(chan struct{}, 2)
	listener := newMockListener()
	listener.acceptFn = func() (net.Conn, error) {
		counter++
		// release the test to complete
		chDone <- struct{}{}
		return nil, tempErr("keep waiting")
	}

	// This should wait (since all tokens are currently in use)
	go n.accept(context.TODO(), listener, pending)

	// Now release one connection, releasing one token
	wg.Done()

	// Wait for it to be released
	wgSuccess.Wait()

	// Wait for the new connection to be accepted
	<-chDone

	// Make sure only one new connection was accepted
	r.Equal(1, counter, "expected exactly one listener to be released")
	n.Shutdown()
}
