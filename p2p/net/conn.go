package net

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"time"

	"fmt"
	"io"
	"net"
	"sync"

	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/net/wire"
	"gopkg.in/op/go-logging.v1"
	"sync/atomic"
)

var (
	// ErrClosedIncomingChannel is sent when the connection is closed because the underlying formatter incoming channel was closed
	ErrClosedIncomingChannel = errors.New("unexpected closed incoming channel")
	// ErrConnectionClosed is sent when the connection is closed after Close was called
	ErrConnectionClosed = errors.New("connections was intentionally closed")
)

// ConnectionSource specifies the connection originator - local or remote node.
type ConnectionSource int

// ConnectionSource values
const (
	Local ConnectionSource = iota
	Remote
)

// Connection is an interface stating the API of all secured connections in the system
type Connection interface {
	fmt.Stringer

	ID() string
	RemotePublicKey() p2pcrypto.PublicKey
	SetRemotePublicKey(key p2pcrypto.PublicKey)

	RemoteAddr() net.Addr

	Session() NetworkSession
	SetSession(session NetworkSession)

	Send(m []byte) error
	Close()
	Closed() bool
}

// FormattedConnection is an io.Writer and an io.Closer
// A network connection supporting full-duplex messaging
type FormattedConnection struct {
	logger *logging.Logger
	// metadata for logging / debugging
	id         string // uuid for logging
	created    time.Time
	remotePub  p2pcrypto.PublicKey
	remoteAddr net.Addr
	closeChan  chan struct{}
	formatter  wire.Formatter // format messages in some way
	networker  networker      // network context
	session    NetworkSession
	closeOnce  sync.Once
	closed     int32
}

type networker interface {
	HandlePreSessionIncomingMessage(c Connection, msg []byte) error
	EnqueueMessage(ime IncomingMessageEvent)
	SubscribeClosingConnections() chan Connection
	publishClosingConnection(c Connection)
	NetworkID() int8
}

type readWriteCloseAddresser interface {
	io.ReadWriteCloser
	RemoteAddr() net.Addr
}

// Create a new connection wrapping a net.Conn with a provided connection manager
func newConnection(conn readWriteCloseAddresser, netw networker, formatter wire.Formatter,
	remotePub p2pcrypto.PublicKey, session NetworkSession, log *logging.Logger) *FormattedConnection {

	// todo parametrize channel size - hard-coded for now
	connection := &FormattedConnection{
		logger:     log,
		id:         crypto.UUIDString(),
		created:    time.Now(),
		remotePub:  remotePub,
		remoteAddr: conn.RemoteAddr(),
		formatter:  formatter,
		networker:  netw,
		session:    session,
		closeChan:  make(chan struct{}),
		closed:     0,
	}

	connection.formatter.Pipe(conn)
	return connection
}

// ID returns the channel's ID
func (c *FormattedConnection) ID() string {
	return c.id
}

// RemoteAddr returns the channel's remote peer address
func (c *FormattedConnection) RemoteAddr() net.Addr {
	return c.remoteAddr
}

// SetRemotePublicKey sets the remote peer's public key
func (c *FormattedConnection) SetRemotePublicKey(key p2pcrypto.PublicKey) {
	c.remotePub = key
}

// RemotePublicKey returns the remote peer's public key
func (c *FormattedConnection) RemotePublicKey() p2pcrypto.PublicKey {
	return c.remotePub
}

// SetSession sets the network session
func (c *FormattedConnection) SetSession(session NetworkSession) {
	c.session = session
}

// Session returns the network session
func (c *FormattedConnection) Session() NetworkSession {
	return c.session
}

// String returns a string describing the connection
func (c *FormattedConnection) String() string {
	return c.id
}

func (c *FormattedConnection) publish(message []byte) {
	c.networker.EnqueueMessage(IncomingMessageEvent{c, message})
}

// incomingChannel returns the incoming messages channel
func (c *FormattedConnection) incomingChannel() chan []byte {
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
	if atomic.LoadInt32(&c.closed) != 1 {
		atomic.StoreInt32(&c.closed, 1)
		close(c.closeChan)
	}
}

// Closed Reports whether the connection was closed. It is go safe.
func (c *FormattedConnection) Closed() bool {
	return atomic.LoadInt32(&c.closed) > 0
}

func (c *FormattedConnection) shutdown(err error) {
	c.logger.Info("(%v) shutdown. id=%s err=%v", c.remotePub.String(), c.id, err)
	if atomic.LoadInt32(&c.closed) != 1 {
		atomic.StoreInt32(&c.closed,1)
		// whoever uses Close knows this connection was closed.
		c.networker.publishClosingConnection(c)
	}
	c.formatter.Close()
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
				err = ErrClosedIncomingChannel
				break Loop
			}

			if c.session == nil {
				err = c.networker.HandlePreSessionIncomingMessage(c, msg)
				if err != nil {
					break Loop
				}
			} else {
				c.publish(msg)
			}

		case <-c.closeChan:
			err = ErrConnectionClosed
			break Loop
		}
	}
	c.shutdown(err)
}
