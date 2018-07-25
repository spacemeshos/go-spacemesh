package net

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"gopkg.in/op/go-logging.v1"
	"net"
	"sync/atomic"
	"time"
)

type ReadWriteCloserMock struct {
}

func (m ReadWriteCloserMock) Read(p []byte) (n int, err error) {
	return 0, nil
}

func (m ReadWriteCloserMock) Write(p []byte) (n int, err error) {
	return 0, nil
}

func (m ReadWriteCloserMock) Close() error {
	return nil
}

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
	logger           *logging.Logger
}

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

func (n *NetworkMock) SetDialResult(err error) {
	n.dialErr = err
}

func (n *NetworkMock) SetDialDelayMs(delay int8) {
	n.dialDelayMs = delay
}

func (n *NetworkMock) Dial(address string, remotePublicKey crypto.PublicKey, networkId int8) (Connection, error) {
	n.networkId = networkId
	atomic.AddInt32(&n.dialCount, 1)
	time.Sleep(time.Duration(n.dialDelayMs) * time.Millisecond)
	conn := NewConnectionMock(remotePublicKey, Local)
	return conn, n.dialErr
}

func (n *NetworkMock) GetDialCount() int32 {
	return n.dialCount
}

func (n *NetworkMock) SubscribeOnNewRemoteConnections() chan Connection {
	ch := make(chan Connection, 20)
	n.regNewRemoteConn = append(n.regNewRemoteConn, ch)
	return ch
}

func (n NetworkMock) PublishNewRemoteConnection(conn Connection) {
	for _, ch := range n.regNewRemoteConn {
		ch <- conn
	}
}

func (n *NetworkMock) setNetworkId(id int8) {
	n.networkId = id
}

func (n *NetworkMock) GetNetworkId() int8 {
	return n.networkId
}

func (n *NetworkMock) ClosingConnections() chan Connection {
	return n.closingConn
}

func (n *NetworkMock) IncomingMessages() chan IncomingMessageEvent {
	return n.incomingMessages
}

func (n NetworkMock) PublishClosingConnection(conn Connection) {
	go func() {
		n.closingConn <- conn
	}()
}

func (n *NetworkMock) SetPreSessionResult(err error) {
	n.preSessionErr = err
}

func (n NetworkMock) GetPreSessionCount() int32 {
	return n.preSessionCount
}

func (n *NetworkMock) HandlePreSessionIncomingMessage(c Connection, msg []byte) error {
	atomic.AddInt32(&n.preSessionCount, 1)
	return n.preSessionErr
}

func (n *NetworkMock) GetLogger() *logging.Logger {
	return n.logger
}
