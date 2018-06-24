package net

import (
	"net"
	"time"

	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/delimited"
	"gopkg.in/op/go-logging.v1"
)

// Connection is a closeable network connection, that can send and receive messages from a remote instance.
// Connection is an io.Writer and an io.Closer.
/*type Connection interface {
	ID() string
	Send(message []byte, id []byte)
	Close() error
	RemoteAddr() net.Addr
}*/

// MessageSentEvent specifies a sent network message data.
type MessageSentEvent struct {
	Connection *Connection
	ID         []byte
}

// IncomingMessage specifies incoming network message data.
type IncomingMessage struct {
	Connection *Connection
	Message    []byte
}

// ConnectionError specifies a connection error.
type ConnectionError struct {
	Connection *Connection
	Err        error
	ID         []byte // optional outgoing message id
}

// MessageSendError defines an error condition for sending a message.
type MessageSendError struct {
	Connection *Connection
	Message    []byte
	Err        error
	ID         []byte
}

// OutgoingMessage specifies an outgoing message data.
type OutgoingMessage struct {
	Message []byte
	ID      []byte
}

// ConnectionSource specifies the connection originator - local or remote node.
type ConnectionSource int

// ConnectionSource values
const (
	Local ConnectionSource = iota
	Remote
)

// A network connection supporting full-duplex messaging
type Connection struct {
	logger *logging.Logger
	// metadata for logging / debugging
	id      string           // uuid for logging
	source  ConnectionSource // remote or local
	created time.Time
	remotePub crypto.PublicKey

	closeChan chan struct{}

	incomingMsgs *delimited.Chan // implemented using delimited.Chan - incoming messages
	outgoingMsgs *delimited.Chan // outgoing message queue

	conn net.Conn // wrapped network connection
	net  Net      // network context

	session NetworkSession
}

// Create a new connection wrapping a net.Conn with a provided connection manager
func newConnection(conn net.Conn, n Net, s ConnectionSource, remotePub crypto.PublicKey) *Connection {

	// todo parametrize incoming msgs chan buffer size - hard-coded for now

	// msgio channel used for incoming message
	incomingMsgs := delimited.NewChan(10)
	outgoingMsgs := delimited.NewChan(10)

	connection := &Connection{
		logger:       n.GetLogger(),
		id:           crypto.UUIDString(),
		created:      time.Now(),
		remotePub:	  remotePub,
		source:       s,
		incomingMsgs: incomingMsgs,
		outgoingMsgs: outgoingMsgs,
		conn:         conn,
		net:          n,
		closeChan:    make(chan struct{}),
	}

	// start reading incoming message from the connection and into the channel
	go incomingMsgs.ReadFromReader(conn)
	// write to the connection through the delimited writer
	go outgoingMsgs.WriteToWriter(conn)

	// start processing channel-based message
	//TODO should be called explicitly by net
	//go connection.beginEventProcessing()
	return connection
}

func (c *Connection) ID() string {
	return c.id
}

// RemoteAddr returns the remote network address.
func (c *Connection) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c Connection) RemotePublicKey() crypto.PublicKey {
	return c.remotePub
}

func (c Connection) Source() ConnectionSource {
	return c.source
}

func (c *Connection) Session() NetworkSession {
	return c.session
}

func (c *Connection) String() string {
	return c.id
}

// Send binary data to a connection
// data is copied over so caller can get rid of the data
// Concurrency: can be called from any go routine
func (c *Connection) Send(message []byte, id []byte) {
	//TODO add err callback
	// queue messages for sending
	c.outgoingMsgs.MsgChan <- delimited.MsgAndID{ID: id, Msg: message}
}

// Close closes the connection (implements io.Closer). It is go safe.
func (c *Connection) Close() error {
	c.incomingMsgs.Close()
	c.outgoingMsgs.Close()
	go func() { c.closeChan <- struct{}{} }()
	return nil
}

// Push outgoing message to the connections
// Read from the incoming new messages and send down the connection
func (c *Connection) beginEventProcessing() {

Loop:
	for {
		select {

		case err := <-c.outgoingMsgs.ErrChan:
			go func() {
				c.net.GetConnectionErrors() <- ConnectionError{c, err.Err, err.ID}
			}()

		case msg, ok := <-c.incomingMsgs.MsgChan:

			if !ok { // chan closed
				break Loop
			}

			// pump the message to the network
			go func() {
				if c.session == nil {
					c.net.GetLogger().Info("DEBUG: got pre session message")
					// dedicated channel for handshake requests
					c.net.GetPreSessionIncomingMessages() <- IncomingMessage{c, msg.Msg}
				} else {
					// channel for protocol messages
					c.net.GetIncomingMessage() <- IncomingMessage{c, msg.Msg}
				}
			}()

		case <-c.closeChan:
			go func() {
				c.net.GetClosingConnections() <- c
			}()
			break Loop

		case err := <-c.incomingMsgs.ErrChan:
			go func() {
				c.net.GetConnectionErrors() <- ConnectionError{c, err.Err, err.ID}
			}()
		}
	}
}
