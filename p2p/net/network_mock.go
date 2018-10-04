package net

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"gopkg.in/op/go-logging.v1"
	"net"
	"sync/atomic"
	"time"
	"math/rand"
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
		panic(err)
	}
	return r
}

func getTestLogger(name string) *logging.Logger {
	return log.New(name, "", "").Logger
}

// NetworkMock is a mock struct
type NetworkMock struct {
	dialErr          error
	dialDelayMs      int8
	dialCount        int32
	preSessionErr    error
	preSessionCount  int32
	regNewRemoteConn []chan Connection
	networkId        int8
	closingConn      chan Connection
	incomingMessages chan IncomingMessageEvent
	dialSessionID    []byte
	logger           *logging.Logger
}

// NewNetworkMock is a mock
func NewNetworkMock() *NetworkMock {
	return &NetworkMock{
		regNewRemoteConn: make([]chan Connection, 0),
		closingConn:      make(chan Connection, 20),
		logger:           getTestLogger("network mock"),
		incomingMessages: make(chan IncomingMessageEvent),
	}
}

func (n *NetworkMock) reset() {
	n.dialCount = 0
	n.dialDelayMs = 0
	n.dialErr = nil
}

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
func (n *NetworkMock) Dial(address string, remotePublicKey crypto.PublicKey) (Connection, error) {
	atomic.AddInt32(&n.dialCount, 1)
	time.Sleep(time.Duration(n.dialDelayMs) * time.Millisecond)
	sID := n.dialSessionID
	if sID == nil {
		sID = make([]byte, 4)
		rand.Read(sID)
	}
	conn := NewConnectionMock(remotePublicKey)
	conn.SetSession(SessionMock{id: sID})
	return conn, n.dialErr
}

// DialCount gets the dial count
func (n *NetworkMock) DialCount() int32 {
	return atomic.LoadInt32(&n.dialCount)
}

// SubscribeOnNewRemoteConnections subscribes on new connections
func (n *NetworkMock) SubscribeOnNewRemoteConnections() chan Connection {
	ch := make(chan Connection, 20)
	n.regNewRemoteConn = append(n.regNewRemoteConn, ch)
	return ch
}

// PublishNewRemoteConnection and stuff
func (n NetworkMock) PublishNewRemoteConnection(conn Connection) {
	for _, ch := range n.regNewRemoteConn {
		ch <- conn
	}
}

func (n *NetworkMock) setNetworkId(id int8) {
	n.networkId = id
}

// NetworkID is netid
func (n *NetworkMock) NetworkID() int8 {
	return n.networkId
}

// ClosingConnections closes connections
func (n *NetworkMock) ClosingConnections() chan Connection {
	return n.closingConn
}

// IncomingMessages return channel of IncomingMessages
func (n *NetworkMock) IncomingMessages() chan IncomingMessageEvent {
	return n.incomingMessages
}

// PublishClosingConnection does just that
func (n NetworkMock) PublishClosingConnection(conn Connection) {
	go func() {
		n.closingConn <- conn
	}()
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
func (n *NetworkMock) Logger() *logging.Logger {
	return n.logger
}
