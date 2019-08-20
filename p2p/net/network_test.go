package net

import (
	"fmt"
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
	ln, err := node.NewNodeIdentity(cfg, "0.0.0.0:0000", false)
	assert.NoError(t, err)
	n, err := NewNet(cfg, ln)
	assert.NoError(t, err)
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	var wg sync.WaitGroup
	for i := 0; i < testnodes; i++ {
		wg.Add(1)
		go func() {
			rnode := node.GenerateRandomNodeData()
			sum := sumByteArray(rnode.PublicKey().Bytes())
			msg := make([]byte, 10, 10)
			rnd.Read(msg)
			n.EnqueueMessage(IncomingMessageEvent{NewConnectionMock(rnode.PublicKey()), msg})
			s := <-n.IncomingMessages()[sum%n.queuesCount]
			assert.Equal(t, s.Message, msg)
			assert.Equal(t, s.Conn.RemotePublicKey(), rnode.PublicKey())
			wg.Done()
		}()
	}
	wg.Wait()
}

type mockListener struct {
	calledCount  int32
	connReleaser chan struct{}
	accpetResErr error
}

func newMockListener() *mockListener {
	return &mockListener{connReleaser: make(chan struct{})}
}

func (ml *mockListener) listenerFunc() (net.Listener, error) {
	return ml, nil
}

func (ml *mockListener) Accept() (net.Conn, error) {
	<-ml.connReleaser
	atomic.AddInt32(&ml.calledCount, 1)
	<-ml.connReleaser
	var c net.Conn = nil
	if ml.accpetResErr == nil {
		c, _ = net.Pipe() // just for the interface lolz
	}
	return c, ml.accpetResErr
}

func (ml *mockListener) releaseConn() {
	ml.connReleaser <- struct{}{}
	ml.connReleaser <- struct{}{}
}

func (ml *mockListener) Close() error {
	return nil
}
func (ml *mockListener) Addr() net.Addr {
	return &net.IPAddr{IP: net.ParseIP("0.0.0.0"), Zone: "ipv4"}
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

	ln, err := node.NewNodeIdentity(cfg, "0.0.0.0:0000", false)
	require.NoError(t, err)
	n, err := NewNet(cfg, ln)
	//n.SubscribeOnNewRemoteConnections(counter)
	listener := newMockListener()
	err = n.listen(listener.listenerFunc)
	require.NoError(t, err)
	listener.accpetResErr = tempErr("demo connection will close and allow more")
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
}

func TestHandlePreSessionIncomingMessage2(t *testing.T) {
	r := require.New(t)
	var wg sync.WaitGroup

	aliceNode, _ := node.GenerateTestNode(t)
	bobNode, _ := node.GenerateTestNode(t)

	bobsAliceConn := NewConnectionMock(aliceNode.PublicKey())
	bobsAliceConn.addr = fmt.Sprintf("%v:%v", aliceNode.IP.String(), aliceNode.ProtocolPort)
	bobsNet, err := NewNet(config.DefaultConfig(), bobNode)
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
}
