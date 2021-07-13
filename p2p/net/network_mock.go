package net

import (
	"context"
	"net"
	"sync/atomic"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/rand"
)

// ReadWriteCloserMock is a mock of ReadWriteCloserMock
type ReadWriteCloserMock struct {
}

// Read reads something
func (m ReadWriteCloserMock) Read(p []byte) (n int, err error) {
	return 0, nil
}

// Write mocks write
func (m ReadWriteCloserMock) Write(p []byte) (n int, err error) {
	return 0, nil
}

// Close mocks close
func (m ReadWriteCloserMock) Close() error {
	return nil
}

//RemoteAddr mocks remote addr return
func (m ReadWriteCloserMock) RemoteAddr() net.Addr {
	r, err := net.ResolveTCPAddr("tcp", "127.0.0.0")
	if err != nil {
		log.Panic("RemoteAddr panicked: ", err)
	}
	return r
}

func getTestLogger(name string) log.Log {
	return log.NewDefault(name)
}

// NetworkMock is a mock struct
type NetworkMock struct {
	dialErr          error
	dialDelayMs      int8
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

// NewNetworkMock is a mock
func NewNetworkMock() *NetworkMock {
	return &NetworkMock{
		regNewRemoteConn: make([]func(NewConnectionEvent), 0, 3),
		closingConn:      make([]func(context.Context, ConnectionWithErr), 0, 3),
		logger:           getTestLogger("network mock"),
		incomingMessages: []chan IncomingMessageEvent{make(chan IncomingMessageEvent, 256)},
	}
}

// SetNextDialSessionID mutates the mock to change the next returned session id
func (n *NetworkMock) SetNextDialSessionID(sID []byte) {
	n.dialSessionID = sID
}

// SetDialResult is a mock
func (n *NetworkMock) SetDialResult(err error) {
	n.dialErr = err
}

// SetDialDelayMs sets delay
func (n *NetworkMock) SetDialDelayMs(delay int8) {
	n.dialDelayMs = delay
}

// Dial dials
func (n *NetworkMock) Dial(ctx context.Context, address net.Addr, remotePublicKey p2pcrypto.PublicKey) (Connection, error) {
	atomic.AddInt32(&n.dialCount, 1)
	select {
	case <-time.After(time.Duration(n.dialDelayMs) * time.Millisecond):
		break
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	sID := n.dialSessionID
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
	return conn, n.dialErr
}

// DialCount gets the dial count
func (n *NetworkMock) DialCount() int32 {
	return atomic.LoadInt32(&n.dialCount)
}

// SubscribeOnNewRemoteConnections subscribes on new connections
func (n *NetworkMock) SubscribeOnNewRemoteConnections(f func(event NewConnectionEvent)) {
	n.regNewRemoteConn = append(n.regNewRemoteConn, f)
}

// PublishNewRemoteConnection and stuff
func (n NetworkMock) PublishNewRemoteConnection(nce NewConnectionEvent) {
	for _, f := range n.regNewRemoteConn {
		f(nce)
	}
}

// SubscribeClosingConnections subscribes on new connections
func (n *NetworkMock) SubscribeClosingConnections(f func(context.Context, ConnectionWithErr)) {
	n.closingConn = append(n.closingConn, f)
}

// publishClosingConnection and stuff
func (n NetworkMock) publishClosingConnection(con ConnectionWithErr) {
	for _, f := range n.closingConn {
		f(context.TODO(), con)
	}
}

// PublishClosingConnection is a hack to expose the above method in the mock but still impl the same interface
func (n NetworkMock) PublishClosingConnection(con ConnectionWithErr) {
	n.publishClosingConnection(con)
}

// NetworkID is netid
func (n *NetworkMock) NetworkID() uint32 {
	return n.networkID
}

// IncomingMessages return channel of IncomingMessages
func (n *NetworkMock) IncomingMessages() []chan IncomingMessageEvent {
	return n.incomingMessages
}

// EnqueueMessage return channel of IncomingMessages
func (n *NetworkMock) EnqueueMessage(ctx context.Context, event IncomingMessageEvent) {
	n.incomingMessages[0] <- event
}

// SetPreSessionResult does this
func (n *NetworkMock) SetPreSessionResult(err error) {
	n.preSessionErr = err
}

// PreSessionCount counts
func (n NetworkMock) PreSessionCount() int32 {
	return atomic.LoadInt32(&n.preSessionCount)
}

// HandlePreSessionIncomingMessage and stuff
func (n *NetworkMock) HandlePreSessionIncomingMessage(c Connection, msg []byte) error {
	atomic.AddInt32(&n.preSessionCount, 1)
	return n.preSessionErr
}

// Logger return the logger
func (n *NetworkMock) Logger() log.Log {
	return n.logger
}
