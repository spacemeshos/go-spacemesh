package net

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/net/wire/delimited"
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
	Close() error
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
	deadline   time.Duration
	timeout    time.Duration
	r          formattedReader
	wmtx       sync.Mutex
	w          formattedWriter
	closed     bool
	deadliner  deadliner
	close      io.Closer

	msgSizeLimit int
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
	deadliner
	RemoteAddr() net.Addr
}

type deadliner interface {
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

type formattedReader interface {
	Next() ([]byte, error)
}

type formattedWriter interface {
	WriteRecord([]byte) (int, error)
}

// Create a new connection wrapping a net.Conn with a provided connection manager
func newConnection(conn readWriteCloseAddresser, netw networker,
	remotePub p2pcrypto.PublicKey, session NetworkSession, msgSizeLimit int, deadline time.Duration, log log.Log) *FormattedConnection {

	// todo parametrize channel size - hard-coded for now
	connection := &FormattedConnection{
		logger:       log,
		id:           crypto.UUIDString(),
		created:      time.Now(),
		remotePub:    remotePub,
		remoteAddr:   conn.RemoteAddr(),
		r:            delimited.NewReader(conn),
		w:            delimited.NewWriter(conn),
		close:        conn,
		deadline:     deadline,
		deadliner:    conn,
		networker:    netw,
		session:      session,
		msgSizeLimit: msgSizeLimit,
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

// NOTE: this is left here intended to help debugging in the future.
//func (c *FormattedConnection) measureSend() context.CancelFunc {
//	ctx, cancel := context.WithCancel(context.Background())
//	go func(ctx context.Context) {
//		timer := time.NewTimer(time.Second * 20)
//		select {
//		case <-timer.C:
//			i := crypto.UUIDString()
//			c.logger.With().Info("sending message is taking more than 20 seconds", log.String("peer", c.RemotePublicKey().String()), log.String("file", fmt.Sprintf("/tmp/stacktrace%v", i)))
//			buf := make([]byte, 1024)
//			for {
//				n := runtime.Stack(buf, true)
//				if n < len(buf) {
//					break
//				}
//				buf = make([]byte, 2*len(buf))
//			}
//			err := ioutil.WriteFile(fmt.Sprintf("/tmp/stacktrace%v", i), buf, 0644)
//			if err != nil {
//				c.logger.Error("ERR WIRTING FILE %v", err)
//			}
//		case <-ctx.Done():
//			return
//		}
//	}(ctx)
//	return cancel
//}

func (c *FormattedConnection) Send(m []byte) error {
	c.wmtx.Lock()
	defer c.wmtx.Unlock()
	if c.closed {
		return fmt.Errorf("connection was closed")
	}

	c.deadliner.SetWriteDeadline(time.Now().Add(c.deadline))
	_, err := c.w.WriteRecord(m)
	if err != nil {
		cerr := c.closeUnlocked()
		if cerr != ErrAlreadyClosed {
			c.networker.publishClosingConnection(ConnectionWithErr{c, err}) // todo: reconsider
		}
		return err
	}
	metrics.PeerRecv.With(metrics.PeerIdLabel, c.remotePub.String()).Add(float64(len(m)))
	return nil
}

var ErrAlreadyClosed = errors.New("connection is already closed")

func (c *FormattedConnection) closeUnlocked() error {
	if c.closed {
		return ErrAlreadyClosed
	}
	err := c.close.Close()
	c.closed = true
	if err != nil {
		return err
	}
	return nil
}

// Close closes the connection (implements io.Closer). It is go safe.
func (c *FormattedConnection) Close() error {
	c.wmtx.Lock()
	err := c.closeUnlocked()
	c.wmtx.Unlock()
	return err
}

// Closed returns whether the connection is closed
func (c *FormattedConnection) Closed() bool {
	c.wmtx.Lock()
	defer c.wmtx.Unlock()
	return c.closed
}

var ErrTriedToSetupExistingConn = errors.New("tried to setup existing connection")
var ErrIncomingSessionTimeout = errors.New("timeout waiting for handshake message")
var ErrMsgExceededLimit = errors.New("message size exceeded limit")

func (c *FormattedConnection) setupIncoming(timeout time.Duration) error {
	be := make(chan struct {
		b []byte
		e error
	})

	go func() {
		// TODO: some other way to make sure this groutine closes
		c.deadliner.SetReadDeadline(time.Now().Add(timeout))
		msg, err := c.r.Next()
		c.deadliner.SetReadDeadline(time.Time{}) // disable read deadline
		be <- struct {
			b []byte
			e error
		}{b: msg, e: err}
	}()

	msgbe := <-be
	msg := msgbe.b
	err := msgbe.e

	if err != nil {
		c.Close()
		return err
	}

	if c.msgSizeLimit != config.UnlimitedMsgSize && len(msg) > c.msgSizeLimit {
		c.logger.With().Error("setupIncoming: message is too big",
			log.Int("limit", c.msgSizeLimit), log.Int("actual", len(msg)))
		return ErrMsgExceededLimit
	}

	if c.session != nil {
		c.Close()
		return errors.New("setup connection twice")
	}

	err = c.networker.HandlePreSessionIncomingMessage(c, msg)
	if err != nil {
		c.Close()
		return err
	}

	return nil
}

// Push outgoing message to the connections
// Read from the incoming new messages and send down the connection
func (c *FormattedConnection) beginEventProcessing() {
	//TODO: use a buffer pool
	var err error
	var buf []byte
	for {
		buf, err = c.r.Next()
		if err != nil && err != io.EOF {
			break
		}

		if c.session == nil {
			err = ErrTriedToSetupExistingConn
			break
		}

		if len(buf) > 0 {
			newbuf := make([]byte, len(buf))
			copy(newbuf, buf)
			c.publish(newbuf)
		}

		if err != nil {
			break
		}
	}

	cerr := c.Close()
	if cerr != ErrAlreadyClosed {
		c.networker.publishClosingConnection(ConnectionWithErr{c, err})
	}
}
