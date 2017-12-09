package p2p2

import (
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p2/msgio"
	"net"
	"time"
)

// A closeable network connection, that can send and recive messages from a remote instance
// Connection is an io.Writer and an  io.Closer
type Connection interface {
	Write(message []byte) (int, error)
	Close() error
	LastOpTime() time.Time 	// last rw op time for this connection
}

// A network connection supporting full-duplex messaging
type connection struct {
	ioChannel *msgio.Chan // implemented using msgio.Chan
	conn      net.Conn    // wrapped network connection
	network   *network    // network context
	lastOpTime time.Time
}

// Send a binary message to the connection remote endpoint
// io.Writer impl
func (c *connection) Write(message []byte) (int, error) {
	n, err := c.conn.Write(message)
	c.lastOpTime = time.Now()
	return n, err
}

// Close the connection (implements io.Closer)
func (c *connection) Close() error {
	return c.Close()
}

func (c *connection) LastOpTime() time.Time {
	return c.lastOpTime
}

// Create a new connection wrapping a net.Conn with a provided connection manager
func newConnection(conn net.Conn, n *network) Connection {
	connection := &connection{
		ioChannel: msgio.NewChanWithRWC(10, conn, conn),
		conn:      conn,
		network:   n,
	}

	// start listening
	go connection.listen()

	return connection
}

// Read client data from channel
func (c *connection) listen() {
Loop:
	for {
		select {
		case msg := <-c.ioChannel.MsgChan:
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
