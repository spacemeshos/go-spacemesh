package net

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"net"
	"sync/atomic"
	"time"
)

// ConnectionMock mocks connections.
type ConnectionMock struct {
	id        string
	remotePub p2pcrypto.PublicKey
	session   NetworkSession

	created time.Time

	Addr net.Addr

	sendDelayMs int
	sendRes     error
	sendCnt     int32

	closed bool

	eventProcessing func()
}

// NewConnectionMock creates a ConnectionMock.
func NewConnectionMock(key p2pcrypto.PublicKey) *ConnectionMock {
	return &ConnectionMock{
		id:        crypto.UUIDString(),
		remotePub: key,
		closed:    false,
		created:   time.Now(),
	}
}

// ID mocks the connection interface.
func (cm ConnectionMock) ID() string {
	return cm.id
}

// Created mocks the connection interface.
func (cm ConnectionMock) Created() time.Time {
	return cm.created
}

// SetCreated mutate the mock.
func (cm ConnectionMock) SetCreated(time2 time.Time) {
	cm.created = time2
}

// RemotePublicKey mocks the interface.
func (cm ConnectionMock) RemotePublicKey() p2pcrypto.PublicKey {
	return cm.remotePub
}

// SetRemotePublicKey mutates the mock.
func (cm *ConnectionMock) SetRemotePublicKey(key p2pcrypto.PublicKey) {
	cm.remotePub = key
}

// RemoteAddr mocks the interface.
func (cm *ConnectionMock) RemoteAddr() net.Addr {
	return cm.Addr
}

// SetSession mutates the mock.
func (cm *ConnectionMock) SetSession(session NetworkSession) {
	cm.session = session
}

// Session mocks the interface.
func (cm ConnectionMock) Session() NetworkSession {
	return cm.session
}

// IncomingChannel mocks the interface.
func (cm ConnectionMock) IncomingChannel() chan []byte {
	return nil
}

// SetSendDelay mutates the mock.
func (cm *ConnectionMock) SetSendDelay(delayMs int) {
	cm.sendDelayMs = delayMs
}

// SetSendResult mutates the mock.
func (cm *ConnectionMock) SetSendResult(err error) {
	cm.sendRes = err
}

// SendCount mutates the mock.
func (cm ConnectionMock) SendCount() int32 {
	return atomic.LoadInt32(&cm.sendCnt)
}

// Send mocks the interface.
func (cm *ConnectionMock) Send(ctx context.Context, m []byte) error {
	atomic.AddInt32(&cm.sendCnt, int32(1))
	time.Sleep(time.Duration(cm.sendDelayMs) * time.Millisecond)
	return cm.sendRes
}

// Closed mocks the interface.
func (cm ConnectionMock) Closed() bool {
	return cm.closed
}

// Close mocks the interface.
func (cm *ConnectionMock) Close() error {
	if cm.closed {
		return errors.New("already closed")
	}
	cm.closed = true
	return nil
}

func (cm *ConnectionMock) beginEventProcessing(context.Context) {
	if cm.eventProcessing != nil {
		cm.eventProcessing()
	} else {
		// pretend we're doing some complicated processing
		time.Sleep(time.Second * 60)
	}
}

// String mocks the interface
func (cm ConnectionMock) String() string {
	return cm.id
}

// SendSock mocks the interface
func (cm *ConnectionMock) SendSock([]byte) error {
	panic("not implemented")
}

func (cm *ConnectionMock) setupIncoming(context.Context, time.Duration) error {
	// pretend we're negotiating a net connection
	time.Sleep(time.Second * 1)
	return nil
}
