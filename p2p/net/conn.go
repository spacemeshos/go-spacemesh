package net

import (
	"encoding/binary"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p/msgio"
	"github.com/google/uuid"
	"net"
	"time"
)

// A closeable network connection, that can send and receive messages from a remote instance
// Connection is an io.Writer and an  io.Closer
type Connection interface {
	Id() string
	Send(message []byte, id []byte)
	Close() error
	LastOpTime() time.Time // last rw op time for this connection
}

type MessageSentEvent struct {
	Connection Connection
	Id         []byte
}

type IncomingMessage struct {
	Connection Connection
	Message    []byte
}

type ConnectionError struct {
	Connection Connection
	Err        error
	Id         []byte // optional outgoing message id
}

type MessageSendError struct {
	Connection Connection
	Message    []byte
	Err        error
	Id         []byte
}

type OutgoingMessage struct {
	Message []byte
	Id      []byte
}

type ConnectionSource int

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

	incomingMsgs *msgio.Chan          // implemented using msgio.Chan - incoming messages
	outgoingMsgs chan OutgoingMessage // outgoing message queue

	conn net.Conn // wrapped network connection
	net  Net      // network context
}

// Create a new connection wrapping a net.Conn with a provided connection manager
func newConnection(conn net.Conn, n Net, s ConnectionSource) Connection {

	// todo parametrize incoming msgs chan buffer size - hard-coded for now

	// msgio channel used for incoming message
	incomingMsgs := msgio.NewChan(10)

	connection := &connectionImpl{
		id:           uuid.New().String(),
		created:      time.Now(),
		source:       s,
		incomingMsgs: incomingMsgs,
		outgoingMsgs: make(chan OutgoingMessage, 10),
		conn:         conn,
		net:          n,
	}

	// start processing channel-based message
	go connection.beginEventProcessing()

	// start reading incoming message from the connection and into the channel
	go incomingMsgs.ReadFrom(conn)

	return connection
}

func (c *connectionImpl) Id() string {
	return c.id
}

func (c *connectionImpl) String() string {
	return c.id
}

// Send binary data to a connection
// data is copied over so caller can get rid of the data
// Concurrency: can be called from any go routine
func (c *connectionImpl) Send(message []byte, id []byte) {
	// queue messages for sending
	c.outgoingMsgs <- OutgoingMessage{message, id}
}

// Write a message to the network connection
// Concurrency: this message is not go safe it is designed as an internal helper to be
// called from the event processing loop
func (c *connectionImpl) writeMessageToConnection(om OutgoingMessage) {

	// give the write a time window to complete - todo: parameterize this as part of node params

	c.conn.SetWriteDeadline(time.Now().Add(time.Second * 45))

	msg := om.Message
	ul := uint32(len(msg))
	err := binary.Write(c.conn, binary.BigEndian, &ul)
	if err != nil {
		go func() {
			c.net.GetMessageSendErrors() <- MessageSendError{c, msg, err, om.Id}
		}()
		return
	}

	_, err = c.conn.Write(msg)
	if err != nil { // error or conn timeout
		go func() {
			c.net.GetMessageSendErrors() <- MessageSendError{c, msg, err, om.Id}
		}()
		return
	}

	c.lastOpTime = time.Now()

	go func() {
		c.net.GetMessageSentCallback() <- MessageSentEvent{c, om.Id}
	}()

}

// Send a binary message to the connection remote endpoint
// message - any binary data
// Concurrency: Not go safe - designed to be used from a the event processing loop
/*
func (c *connectionImpl) write(message []byte) (int, error) {
	ul := uint32(len(message))
	err := binary.Write(c.conn, binary.BigEndian, &ul)
	n, err := c.conn.Write(message)
	return n + 4, err
}*/

// Close the connection (implements io.Closer)
// go safe
func (c *connectionImpl) Close() error {
	c.incomingMsgs.Close()
	return nil
}

// go safe
func (c *connectionImpl) LastOpTime() time.Time {
	return c.lastOpTime
}

// Push outgoing message to the connections
// Read from the incoming new messages and send down the connection
func (c *connectionImpl) beginEventProcessing() {

Loop:
	for {
		select {

		case msg, ok := <-c.outgoingMsgs:

			if ok { // new outgoing message
				c.writeMessageToConnection(msg)
			}

		case msg, ok := <-c.incomingMsgs.MsgChan:

			if !ok { // chan closed
				break Loop
			}

			log.Info("Incoming message from: %s to: %s.", c.conn.RemoteAddr().String(), c.conn.LocalAddr().String())
			c.lastOpTime = time.Now()

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
