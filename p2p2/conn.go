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

// A closeable network connection, that can send and receive messages from a remote instance
// Connection is an io.Writer and an  io.Closer
type Connection interface {
	Id() string
	Send(message []byte)
	Close() error
	LastOpTime() time.Time // last rw op time for this connection
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

	// todo: add channel for outgoing messages - clients just push messages to this channel

	outgoingMsgs chan []byte

	conn    net.Conn // wrapped network connection
	network *network // network context

	//msgWriter msgio.WriteCloser
	//msgReader msgio.ReadCloser
}

// Create a new connection wrapping a net.Conn with a provided connection manager
func newConnection(conn net.Conn, n *network, s ConnectionSource) Connection {

	// todo parametize incoming msgs chan buffer size - hard-coded for now

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

	// process incoming messages from the channel
	go connection.listen()

	// start reading incoming message from the connection and into the channel
	go msgioChan.ReadFrom(conn)

	return connection
}

func (c *connection) Id() string {
	return c.id
}

func (c *connection) String() string {
	return c.id
}

// Send binary data to a connection
// data is copied over so caller can get rid of the data
func (c *connection) Send(message []byte) {
	// queue messages for sending
	c.outgoingMsgs <- message
}

// Write a message to the network connection
// note: this message is not go safe it is designed as a helper to
// a channel select statement -
func (c *connection) writeMessageToConnection(message *[]byte) {
	ul := uint32(len(*message))
	binary.Write(c.conn, binary.BigEndian, &ul)
	_, err := c.conn.Write(*message)

	if err != nil {
		c.network.messageSendError(c, err)
		return
	}

	c.lastOpTime = time.Now()

}

// Send a binary message to the connection remote endpoint
// message - any binary data
// Not go safe - designed to be used from a sync channel
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

// Read client data from channel
func (c *connection) listen() {

	log.Info("Connection processing channel....")

Loop:
	for {
		select {

		case msg, ok := <-c.outgoingMsgs:
			if ok { // new outgoing message
				c.writeMessageToConnection(&msg)
			}

		case msg, ok := <-c.ioChannel.MsgChan:
			if !ok { // chan closed
				break Loop
			}

			log.Info("New message from remote node: %s", string(msg))
			c.lastOpTime = time.Now()
			c.network.remoteClientMessage(c, msg)

		case <-c.ioChannel.CloseChan:
			c.network.connectionClosed(c)
			break Loop

		case err := <-c.ioChannel.ErrChan:
			c.network.connectionError(c, err)
		}
	}
}
