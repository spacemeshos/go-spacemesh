package net

import (
	"errors"
	"time"

	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/net/wire"
	"gopkg.in/op/go-logging.v1"
	"io"
	"net"
	"sync"
)

var (
	ErrClosedChannel    = errors.New("unexpected closed connection channel")
	ErrConnectionClosed = errors.New("connections was intentionally closed")
)

// ConnectionSource specifies the connection originator - local or remote node.
type ConnectionSource int

// ConnectionSource values
const (
	Local ConnectionSource = iota
	Remote
)

type Connection interface {
	fmt.Stringer

	ID() string
	RemotePublicKey() crypto.PublicKey
	SetRemotePublicKey(key crypto.PublicKey)

	RemoteAddr() net.Addr

	Session() NetworkSession
	SetSession(session NetworkSession)

	IncomingChannel() chan []byte

	Send(m []byte) error
	Close()
}

// A network connection supporting full-duplex messaging
// FormattedConnection is an io.Writer and an io.Closer
type FormattedConnection struct {
	logger *logging.Logger
	// metadata for logging / debugging
	id         string // uuid for logging
	created    time.Time
	remotePub  crypto.PublicKey
	remoteAddr net.Addr
	closeChan  chan struct{}
	formatter  wire.Formatter // format messages in some way
	networker  networker      // network context
	session    NetworkSession
	closeOnce  sync.Once
}

type networker interface {
	HandlePreSessionIncomingMessage(c Connection, msg []byte) error
	IncomingMessages() chan IncomingMessageEvent
	ClosingConnections() chan Connection
	GetNetworkId() int8
}

type readWriteCloseAddresser interface {
	io.ReadWriteCloser
	RemoteAddr() net.Addr
}

// Create a new connection wrapping a net.Conn with a provided connection manager
func newConnection(conn readWriteCloseAddresser, netw networker, formatter wire.Formatter, remotePub crypto.PublicKey, log *logging.Logger) *FormattedConnection {

	// todo pass wire format inside and make it pluggable
	// todo parametrize channel size - hard-coded for now
	connection := &FormattedConnection{
		logger:     log,
		id:         crypto.UUIDString(),
		created:    time.Now(),
		remotePub:  remotePub,
		remoteAddr: conn.RemoteAddr(),
		formatter:  formatter,
		networker:  netw,
		closeChan:  make(chan struct{}),
	}

	connection.formatter.Pipe(conn)
	return connection
}

func (c *FormattedConnection) ID() string {
	return c.id
}

func (c *FormattedConnection) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func (c *FormattedConnection) SetRemotePublicKey(key crypto.PublicKey) {
	c.remotePub = key
}

func (c FormattedConnection) RemotePublicKey() crypto.PublicKey {
	return c.remotePub
}

func (c *FormattedConnection) SetSession(session NetworkSession) {
	c.session = session
}

func (c *FormattedConnection) Session() NetworkSession {
	return c.session
}

func (c *FormattedConnection) String() string {
	return c.id
}

func (c *FormattedConnection) publish(message []byte) {
	c.networker.IncomingMessages() <- IncomingMessageEvent{c, message}
}

func (c *FormattedConnection) IncomingChannel() chan []byte {
	return c.formatter.In()
}

// Send binary data to a connection
// data is copied over so caller can get rid of the data
// Concurrency: can be called from any go routine
func (c *FormattedConnection) Send(m []byte) error {
	return c.formatter.Out(m)
}

// Close closes the connection (implements io.Closer). It is go safe.
func (c *FormattedConnection) Close() {
	c.closeOnce.Do(func() {
		c.closeChan <- struct{}{}
	})
}

func (c *FormattedConnection) shutdown(err error) {
	c.logger.Info("shutdown. err=%v", err)
	c.formatter.Close()
	c.networker.ClosingConnections() <- c
}

// Push outgoing message to the connections
// Read from the incoming new messages and send down the connection
func (c *FormattedConnection) beginEventProcessing() {

	var err error

Loop:
	for {
		select {
		case msg, ok := <-c.formatter.In():

			if !ok { // chan closed
				err = ErrClosedChannel
				break Loop
			}

			if c.session == nil {
				err = c.networker.HandlePreSessionIncomingMessage(c, msg)
				if err != nil {
					break Loop
				}
			} else {
				// channel for protocol messages
				go c.publish(msg)
			}

		case <-c.closeChan:
			err = ErrConnectionClosed
			break Loop
		}
	}
	c.shutdown(err)
}
