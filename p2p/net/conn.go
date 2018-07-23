package net

import (
	"errors"
	"time"

	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/delimited"
	"github.com/spacemeshos/go-spacemesh/p2p/net/wire"
	"gopkg.in/op/go-logging.v1"
	"io"
	"fmt"
	"net"
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

type Connectioner interface {
	fmt.Stringer

	ID() string
	RemotePublicKey() crypto.PublicKey
	SetRemotePublicKey(key crypto.PublicKey)

	RemoteAddr() net.Addr

	Session() NetworkSession
	SetSession(session NetworkSession)

	Source() ConnectionSource

	IncomingChannel() chan wire.InMessage

	Send(m []byte) error
	Close()

	beginEventProcessing()
}

// A network connection supporting full-duplex messaging
// Connection is an io.Writer and an io.Closer
type Connection struct {
	logger *logging.Logger
	// metadata for logging / debugging
	id         string           // uuid for logging
	source     ConnectionSource // remote or local
	created    time.Time
	remotePub  crypto.PublicKey
	remoteAddr net.Addr
	closeChan chan struct{}
	formatter wire.Formatter // format messages in some way
	networker networker               // network context
	session NetworkSession
}

type networker interface {
	HandlePreSessionIncomingMessage(c Connectioner, msg wire.InMessage) error
	IncomingMessages() chan IncomingMessageEvent
	ClosingConnections() chan Connectioner
	GetNetworkId() int8
}

type readWriteCloseAddresser interface {
	io.ReadWriteCloser
	RemoteAddr() net.Addr
}

// Create a new connection wrapping a net.Conn with a provided connection manager
func newConnection(conn readWriteCloseAddresser, netw networker, s ConnectionSource, remotePub crypto.PublicKey, log *logging.Logger) *Connection {

	// todo pass wire format inside and make it pluggable
	// todo parametrize channel size - hard-coded for now
	connection := &Connection{
		logger:     log,
		id:         crypto.UUIDString(),
		created:    time.Now(),
		remotePub:  remotePub,
		remoteAddr: conn.RemoteAddr(),
		formatter:  delimited.NewChan(10),
		source:     s,
		networker:  netw,
		closeChan:  make(chan struct{}),
	}

	connection.formatter.Pipe(conn)
	return connection
}

func (c *Connection) ID() string {
	return c.id
}

func (c *Connection) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func (c *Connection) SetRemotePublicKey(key crypto.PublicKey) {
	c.remotePub = key
}

func (c Connection) RemotePublicKey() crypto.PublicKey {
	return c.remotePub
}

func (c Connection) Source() ConnectionSource {
	return c.source
}

func (c *Connection) SetSession(session NetworkSession) {
	c.session = session
}

func (c *Connection) Session() NetworkSession {
	return c.session
}

func (c *Connection) String() string {
	return c.id
}

func (c *Connection) publish(message wire.InMessage) {
	c.networker.IncomingMessages() <- IncomingMessageEvent{c, message.Message()}
}

func (c *Connection) IncomingChannel() chan wire.InMessage {
	return c.formatter.In()
}

// Send binary data to a connection
// data is copied over so caller can get rid of the data
// Concurrency: can be called from any go routine
func (c *Connection) Send(m []byte) error {
	return wire.Send(c.formatter, m)
}

// Close closes the connection (implements io.Closer). It is go safe.
func (c *Connection) Close() {
	c.closeChan <- struct{}{}
}

func (c *Connection) shutdown(err error) {
	c.logger.Info("shutdown. err=%v", err)
	c.formatter.Close()
	c.networker.ClosingConnections() <- c
}

// Push outgoing message to the connections
// Read from the incoming new messages and send down the connection
func (c *Connection) beginEventProcessing() {

	var err error

Loop:
	for {
		select {
		case msg, ok := <-c.formatter.In():

			if !ok { // chan closed
				err = ErrClosedChannel
				break Loop
			}

			if msg.Error() != nil {
				err = msg.Error()
				break Loop
			}

			if c.session == nil {
				c.logger.Info("DEBUG: got pre session message")
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
