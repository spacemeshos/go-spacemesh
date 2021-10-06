package net

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/rand"
)

// ReadWriteCloserMock is a mock of ReadWriteCloserMock.
type ReadWriteCloserMock struct{}

// Read reads something.
func (m ReadWriteCloserMock) Read(p []byte) (n int, err error) {
	return 0, nil
}

// Write mocks write.
func (m ReadWriteCloserMock) Write(p []byte) (n int, err error) {
	return 0, nil
}

// Close mocks close.
func (m ReadWriteCloserMock) Close() error {
	return nil
}

// RemoteAddr mocks remote addr return.
func (m ReadWriteCloserMock) RemoteAddr() net.Addr {
	r, err := net.ResolveTCPAddr("tcp", "127.0.0.0")
	if err != nil {
		log.Panic("RemoteAddr panicked: ", err)
	}
	return r
}

// NetworkMock is a mock struct.
type NetworkMock struct {
	mu               sync.RWMutex
	dialErr          error
	dialDelay        time.Duration
	dialCount        int32
	preSessionErr    error
	preSessionCount  int32
	regNewRemoteConn []func(NewConnectionEvent)
	networkID        uint32
	closingConn      []func(context.Context, ConnectionWithErr)
	incomingMessages []chan IncomingMessageEvent
	dialSessionID    []byte
	logger           log.Log
}

// NewNetworkMock is a mock.
func NewNetworkMock(tb testing.TB) *NetworkMock {
	return &NetworkMock{
		regNewRemoteConn: make([]func(NewConnectionEvent), 0, 3),
		closingConn:      make([]func(context.Context, ConnectionWithErr), 0, 3),
		logger:           logtest.New(tb).WithName("network mock"),
		incomingMessages: []chan IncomingMessageEvent{make(chan IncomingMessageEvent, 256)},
	}
}

// SetNextDialSessionID mutates the mock to change the next returned session id.
func (n *NetworkMock) SetNextDialSessionID(sID []byte) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.dialSessionID = sID
}

// SetDialResult is a mock.
func (n *NetworkMock) SetDialResult(err error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.dialErr = err
}

// SetDialDelay sets delay.
func (n *NetworkMock) SetDialDelay(delay time.Duration) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.dialDelay = delay
}

// Dial dials.
func (n *NetworkMock) Dial(ctx context.Context, address net.Addr, remotePublicKey p2pcrypto.PublicKey) (Connection, error) {
	// TODO(nkryuchkov): fix data race
	atomic.AddInt32(&n.dialCount, 1)

	n.mu.RLock()
	dialDelay := n.dialDelay
	n.mu.RUnlock()

	select {
	case <-time.After(dialDelay):
		break
	case <-ctx.Done():
		return nil, fmt.Errorf("context done: %w", ctx.Err())
	}

	n.mu.RLock()
	sID := n.dialSessionID
	n.mu.RUnlock()

	if sID == nil {
		sID = make([]byte, 4)
		_, err := rand.Read(sID)
		if err != nil {
			panic("no randomness for networkmock")
		}
	}
	conn := NewConnectionMock(remotePublicKey)
	publicKey, _ := p2pcrypto.NewPubkeyFromBytes(sID)
	conn.SetSession(SessionMock{id: publicKey})

	n.mu.RLock()
	defer n.mu.RUnlock()

	return conn, n.dialErr
}

// DialCount gets the dial count.
func (n *NetworkMock) DialCount() int32 {
	return atomic.LoadInt32(&n.dialCount)
}

// SubscribeOnNewRemoteConnections subscribes on new connections.
func (n *NetworkMock) SubscribeOnNewRemoteConnections(f func(event NewConnectionEvent)) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.regNewRemoteConn = append(n.regNewRemoteConn, f)
}

// PublishNewRemoteConnection and stuff.
func (n *NetworkMock) PublishNewRemoteConnection(nce NewConnectionEvent) {
	n.mu.RLock()
	list := n.regNewRemoteConn
	n.mu.RUnlock()

	for _, f := range list {
		f(nce)
	}
}

// SubscribeClosingConnections subscribes on new connections.
func (n *NetworkMock) SubscribeClosingConnections(f func(context.Context, ConnectionWithErr)) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.closingConn = append(n.closingConn, f)
}

// publishClosingConnection and stuff.
func (n *NetworkMock) publishClosingConnection(con ConnectionWithErr) {
	n.mu.RLock()
	list := n.closingConn
	n.mu.RUnlock()

	for _, f := range list {
		f(context.TODO(), con)
	}
}

// PublishClosingConnection is a hack to expose the above method in the mock but still impl the same interface.
func (n *NetworkMock) PublishClosingConnection(con ConnectionWithErr) {
	n.publishClosingConnection(con)
}

// NetworkID is netid.
func (n *NetworkMock) NetworkID() uint32 {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return n.networkID
}

// IncomingMessages return channel of IncomingMessages.
func (n *NetworkMock) IncomingMessages() []chan IncomingMessageEvent {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return n.incomingMessages
}

// EnqueueMessage return channel of IncomingMessages.
func (n *NetworkMock) EnqueueMessage(ctx context.Context, event IncomingMessageEvent) {
	n.mu.Lock()
	ch := n.incomingMessages[0]
	n.mu.Unlock()

	ch <- event
}

// SetPreSessionResult does this.
func (n *NetworkMock) SetPreSessionResult(err error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.preSessionErr = err
}

// PreSessionCount counts.
func (n *NetworkMock) PreSessionCount() int32 {
	return atomic.LoadInt32(&n.preSessionCount)
}

// HandlePreSessionIncomingMessage and stuff.
func (n *NetworkMock) HandlePreSessionIncomingMessage(c Connection, msg []byte) error {
	atomic.AddInt32(&n.preSessionCount, 1)

	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.preSessionErr
}

// Logger return the logger.
func (n *NetworkMock) Logger() log.Log {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return n.logger
}
