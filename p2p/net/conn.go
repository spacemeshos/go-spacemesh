package net

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/delimited"
	"net"
	"sync"
	"time"
)

// Connection is a closeable network connection, that can send and receive messages from a remote instance.
// Connection is an io.Writer and an io.Closer.
type Connection interface {
	ID() string
	Send(message []byte, id []byte)
	Close() error
	LastOpTime() time.Time // last rw op time for this connection
	RemoteAddr() net.Addr
}

// MessageSentEvent specifies a sent network message data.
type MessageSentEvent struct {
	Connection Connection
	ID         []byte
}

// IncomingMessage specifies incoming network message data.
type IncomingMessage struct {
	Connection Connection
	Message    []byte
}

// ConnectionError specifies a connection error.
type ConnectionError struct {
	Connection Connection
	Err        error
	ID         []byte // optional outgoing message id
}

// MessageSendError defines an error condition for sending a message.
type MessageSendError struct {
	Connection Connection
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
type connectionImpl struct {

	// metadata for logging / debugging
	id         string           // uuid for logging
	source     ConnectionSource // remote or local
	created    time.Time
	lastOpTime time.Time

	incomingMsgs *delimited.Chan // implemented using delimited.Chan - incoming messages
	outgoingMsgs *delimited.Chan // outgoing message queue

	opsWaitGroup sync.WaitGroup

	conn net.Conn // wrapped network connection
	net  Net      // network context
}

// Create a new connection wrapping a net.Conn with a provided connection manager
func newConnection(conn net.Conn, n Net, s ConnectionSource) Connection {

	// todo parametrize incoming msgs chan buffer size - hard-coded for now

	// msgio channel used for incoming message
	incomingMsgs := delimited.NewChan(10)
	outgoingMsgs := delimited.NewChan(10)

	connection := &connectionImpl{
		id:           crypto.UUIDString(),
		created:      time.Now(),
		source:       s,
		incomingMsgs: incomingMsgs,
		outgoingMsgs: outgoingMsgs,
		conn:         conn,
		net:          n,
		opsWaitGroup: sync.WaitGroup{},
	}

	incomingMsgs.RegisterOpCallback(connection.RecordOpTime)
	outgoingMsgs.RegisterOpCallback(connection.RecordOpTime)

	// start reading incoming message from the connection and into the channel
	go incomingMsgs.ReadFromReader(conn)
	// write to the connection through the delimited writer
	go outgoingMsgs.WriteToWriter(conn)
	// start processing channel-based message
	go connection.beginEventProcessing()

	return connection
}

func (c *connectionImpl) ID() string {
	return c.id
}

// RemoteAddr returns the remote network address.
func (c *connectionImpl) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *connectionImpl) String() string {
	return c.id
}

// Send binary data to a connection
// data is copied over so caller can get rid of the data
// Concurrency: can be called from any go routine
func (c *connectionImpl) Send(message []byte, id []byte) {
	// queue messages for sending

	c.outgoingMsgs.MsgChan <- message
}

// Close closes the connection (implements io.Closer). It is go safe.
func (c *connectionImpl) Close() error {
	c.incomingMsgs.Close()
	c.outgoingMsgs.Close()
	return nil
}

// RecordOpTime records now as the last op time
func (c *connectionImpl) RecordOpTime() {
	c.opsWaitGroup.Add(1)
	c.lastOpTime = time.Now()
	c.opsWaitGroup.Done()
}

// LastOpTime returns the last optime that was recorded
func (c *connectionImpl) LastOpTime() time.Time {
	c.opsWaitGroup.Wait()
	return c.lastOpTime
}

// Push outgoing message to the connections
// Read from the incoming new messages and send down the connection
func (c *connectionImpl) beginEventProcessing() {

Loop:
	for {
		select {

		case err := <-c.outgoingMsgs.ErrChan:
			go func() {
				c.net.GetConnectionErrors() <- ConnectionError{c, err, nil}
			}()

		case msg, ok := <-c.incomingMsgs.MsgChan:

			if !ok { // chan closed
				break Loop
			}

			log.Info("Incoming message from: %s to: %s.", c.conn.RemoteAddr().String(), c.conn.LocalAddr().String())

			// pump the message to the network
			go func() {
				c.net.GetIncomingMessage() <- IncomingMessage{c, msg}
			}()

		case <-c.incomingMsgs.CloseChan:
			go func() {
				c.net.GetClosingConnections() <- c
			}()
			break Loop

		case err := <-c.incomingMsgs.ErrChan:
			go func() {
				c.net.GetConnectionErrors() <- ConnectionError{c, err, nil}
			}()
		}
	}
}
