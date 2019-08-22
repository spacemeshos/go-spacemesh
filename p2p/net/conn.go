package net

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/delimited"
	"github.com/spacemeshos/go-spacemesh/p2p/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"time"

	"fmt"
	"io"
	"net"
	"sync"

	"github.com/spacemeshos/go-spacemesh/crypto"
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
	// metadata for logging / debugging
	logger     log.Log
	id         string // uuid for logging
	created    time.Time
	remotePub  p2pcrypto.PublicKey
	remoteAddr net.Addr
	networker  networker // network context
	session    NetworkSession

	r formattedReader

	wmtx sync.Mutex
	w    formattedWriter

	closed bool
	close  io.Closer
}

type networker interface {
	HandlePreSessionIncomingMessage(c Connection, msg []byte) error
	EnqueueMessage(ime IncomingMessageEvent)
	SubscribeClosingConnections(func(c ConnectionWithErr))
	publishClosingConnection(c ConnectionWithErr)
	NetworkID() int8
}

type readWriteCloseAddresser interface {
	io.ReadWriteCloser
	RemoteAddr() net.Addr
}

type formattedReader interface {
	Next() ([]byte, error)
}

type formattedWriter interface {
	WriteRecord([]byte) (int, error)
}

// Create a new connection wrapping a net.Conn with a provided connection manager
func newConnection(conn readWriteCloseAddresser, netw networker,
	remotePub p2pcrypto.PublicKey, session NetworkSession, log log.Log) *FormattedConnection {

	// todo parametrize channel size - hard-coded for now
	connection := &FormattedConnection{
		logger:     log,
		id:         crypto.UUIDString(),
		created:    time.Now(),
		remotePub:  remotePub,
		remoteAddr: conn.RemoteAddr(),
		r:          delimited.NewReader(conn),
		w:          delimited.NewWriter(conn),
		close:      conn,
		networker:  netw,
		session:    session,
	}

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

// Send binary data to a connection
// data is copied over so caller can get rid of the data
// Concurrency: can be called from any go routine
func (c *FormattedConnection) Send(m []byte) error {
	c.wmtx.Lock()
	_, err := c.w.WriteRecord(m)
	c.wmtx.Unlock()
	if err != nil {
		return err
	}
	metrics.PeerRecv.With(metrics.PeerIdLabel, c.remotePub.String()).Add(float64(len(m)))
	return nil
}

// Close closes the connection (implements io.Closer). It is go safe.
func (c *FormattedConnection) Close() {
	c.wmtx.Lock()
	if c.closed {
		c.wmtx.Unlock()
		return
	}
	err := c.close.Close()
	c.closed = true
	c.wmtx.Unlock()
	if err != nil {
		c.logger.Warning("error while closing with connection %v, err: %v", c.RemotePublicKey().String(), err)
	}
}

// Closed returns whether the connection is closed
func (c *FormattedConnection) Closed() bool {
	c.wmtx.Lock()
	defer c.wmtx.Unlock()
	return c.closed
}

var ErrTriedToSetupExistingConn = errors.New("tried to setup existing connection")
var ErrIncomingSessionTimeout = errors.New("timeout waiting for handshake message")

func (c *FormattedConnection) setupIncoming(timeout time.Duration) error {
	be := make(chan struct {
		b []byte
		e error
	})

	go func() {
		// TODO: some other way to make sure this groutine closes
		msg, err := c.r.Next()
		be <- struct {
			b []byte
			e error
		}{b: msg, e: err}
	}()

	t := time.NewTimer(timeout)

	select {
	case msgbe := <-be:
		msg := msgbe.b
		err := msgbe.e

		if err != nil {
			c.Close()
			return err
		}
		if c.session == nil {
			err = c.networker.HandlePreSessionIncomingMessage(c, msg)
			if err != nil {
				c.Close()
				return err
			}
		} else {
			c.Close()
			return errors.New("setup connection twice")
		}
	case <-t.C:
		c.Close()
		return errors.New("timeout while waiting for session message")
	}

	return nil
}

// Push outgoing message to the connections
// Read from the incoming new messages and send down the connection
func (c *FormattedConnection) beginEventProcessing() {
	var err error
	var buf []byte
	for {
		buf, err = c.r.Next()
		if err != nil {
			break
		}

		if c.session == nil {
			err = ErrTriedToSetupExistingConn
			break
		}

		newbuf := make([]byte, len(buf))
		copy(newbuf, buf)
		c.publish(newbuf)
	}

	c.Close()
	c.networker.publishClosingConnection(ConnectionWithErr{c, err})
}
