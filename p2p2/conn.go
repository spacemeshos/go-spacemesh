package p2p2

import (
	"encoding/binary"
	"github.com/google/uuid"

	//"bytes"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p2/msgio"
	"net"
	"time"
)

// Session info with a remote node - wraps connection
type NetworkSession interface {
	key() []byte        // session key
	Created() time.Time // time when session was established
}

type ConnectionMessage struct {
	Connection Connection
	Message    []byte
}

type ConnectionError struct {
	Connection Connection
	err        error
}

type MessageSendError struct {
	Connection Connection
	Message    []byte
	err        error
}

// A closeable network connection, that can send and receive messages from a remote instance
// Connection is an io.Writer and an  io.Closer
type Connection interface {
	Id() string

	Send(message []byte)
	Close() error
	LastOpTime() time.Time // last rw op time for this connection

	// optional established session
	GetSession() NetworkSession
	SetSession(s NetworkSession)
}

type ConnectionSource int

const (
	Local ConnectionSource = iota
	Remote
)

// A network connection supporting full-duplex messaging
type connection struct {

	// metadata for logging / debugging
	id         string           // uuid for logging
	source     ConnectionSource // remote or local
	created    time.Time
	lastOpTime time.Time

	ioChannel *msgio.Chan // implemented using msgio.Chan - incoming messages

	outgoingMsgs chan []byte // outgoing message queue

	conn    net.Conn // wrapped network connection
	network Network  // network context

	session NetworkSession

	//msgWriter msgio.WriteCloser
	//msgReader msgio.ReadCloser
}

// Create a new connection wrapping a net.Conn with a provided connection manager
func newConnection(conn net.Conn, n Network, s ConnectionSource) Connection {

	// todo parametrize incoming msgs chan buffer size - hard-coded for now

	// msgio channel used for incoming message
	msgioChan := msgio.NewChan(10)

	connection := &connection{
		id:           uuid.New().String(),
		created:      time.Now(),
		source:       s,
		ioChannel:    msgioChan,
		outgoingMsgs: make(chan []byte, 10),
		conn:         conn,
		network:      n,
	}

	// start processing channel-based message
	go connection.begingEventProcessing()

	// start reading incoming message from the connection and into the channel
	go msgioChan.ReadFrom(conn)

	return connection
}

// not concurrency safe - attach a session to the connection
func (c *connection) SetSession(s NetworkSession) {
	c.session = s
}

func (c *connection) GetSession() NetworkSession {
	return c.session
}

func (c *connection) Id() string {
	return c.id
}

func (c *connection) String() string {
	return c.id
}

// Send binary data to a connection
// data is copied over so caller can get rid of the data
// Concurrency: can be called from any go routine
func (c *connection) Send(message []byte) {
	// queue messages for sending
	c.outgoingMsgs <- message
}

// Write a message to the network connection
// Concurrency: this message is not go safe it is designed as an internal helper to
// called from the event processing loop
func (c *connection) writeMessageToConnection(msg []byte) {

	// note: using msgio Reader and Writer failed here so this is a temp hack:
	ul := uint32(len(msg))
	err := binary.Write(c.conn, binary.BigEndian, &ul)
	if err != nil {

		c.network.GetMessageSendErrors() <- MessageSendError{c, msg, err}
		return
	}

	_, err = c.conn.Write(msg)
	if err != nil {
		c.network.GetMessageSendErrors() <- MessageSendError{c, msg, err}
		return
	}

	c.lastOpTime = time.Now()
}

// Send a binary message to the connection remote endpoint
// message - any binary data
// Concurrency: Not go safe - designed to be used from a the event processing loop
func (c *connection) write(message []byte) (int, error) {

	ul := uint32(len(message))
	err := binary.Write(c.conn, binary.BigEndian, &ul)
	n, err := c.conn.Write(message)

	return n + 4, err
}

// Close the connection (implements io.Closer)
func (c *connection) Close() error {
	c.ioChannel.Close()
	return nil
}

func (c *connection) LastOpTime() time.Time {
	return c.lastOpTime
}

// Push outgoing message to the connections
// Read from the icnoming new messages and send down the connection

func (c *connection) begingEventProcessing() {

	log.Info("Connection processing channel....")

Loop:
	for {
		select {

		case msg, ok := <-c.outgoingMsgs:
			if ok { // new outgoing message
				c.writeMessageToConnection(msg)
			}

		case msg, ok := <-c.ioChannel.MsgChan:

			if !ok { // chan closed
				break Loop
			}

			log.Info("New message from remote node: %s", string(msg))
			c.lastOpTime = time.Now()

			// pump the message to the network
			c.network.GetIncomingMessage() <- ConnectionMessage{c, msg}

		case <-c.ioChannel.CloseChan:
			c.network.GetClosingConnections() <- c
			break Loop

		case err := <-c.ioChannel.ErrChan:
			c.network.GetConnectionErrors() <- ConnectionError{c, err}
		}
	}
}
