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

// MessageQueueSize is the size for queue of messages before pushing them on the socket
const MessageQueueSize = 250

// Connection is an interface stating the API of all secured connections in the system
type Connection interface {
	fmt.Stringer

	ID() string
	RemotePublicKey() p2pcrypto.PublicKey
	SetRemotePublicKey(key p2pcrypto.PublicKey)
	Created() time.Time

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
	logger      log.Log
	id          string // uuid for logging
	created     time.Time
	remotePub   p2pcrypto.PublicKey
	remoteAddr  net.Addr
	networker   networker // network context
	session     NetworkSession
	deadline    time.Duration
	r           formattedReader
	wmtx        sync.Mutex
	w           formattedWriter
	closed      bool
	deadliner   deadliner
	messages    chan []byte
	stopSending chan struct{}
	close       io.Closer

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
		messages:     make(chan []byte, MessageQueueSize),
		stopSending:  make(chan struct{}),
		msgSizeLimit: msgSizeLimit,
	}
	go connection.sendListener()
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

// Created is the time the connection was created
func (c *FormattedConnection) Created() time.Time {
	return c.created
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

func (c *FormattedConnection) sendListener() {
	for {
		select {
		case buf := <-c.messages:
			//todo: we are hiding the error here...
			err := c.SendSock(buf)
			if err != nil {
				log.Error("cannot send message to peer %v", err)
			}
		case <-c.stopSending:
			return

		}
	}
}

// Send pushes a message into the queue if the connection is not closed.
func (c *FormattedConnection) Send(m []byte) error {
	c.wmtx.Lock()
	if c.closed {
		c.wmtx.Unlock()
		return fmt.Errorf("connection was closed")
	}
	c.wmtx.Unlock()
	c.messages <- m
	return nil
}

// SendSock sends a message directly on the socket without waiting for the queue.
func (c *FormattedConnection) SendSock(m []byte) error {
	c.wmtx.Lock()
	if c.closed {
		c.wmtx.Unlock()
		return fmt.Errorf("connection was closed")
	}

	err := c.deadliner.SetWriteDeadline(time.Now().Add(c.deadline))
	if err != nil {
		return err
	}
	_, err = c.w.WriteRecord(m)
	if err != nil {
		cerr := c.closeUnlocked()
		c.wmtx.Unlock()
		if cerr != ErrAlreadyClosed {
			c.networker.publishClosingConnection(ConnectionWithErr{c, err}) // todo: reconsider
		}
		return err
	}
	c.wmtx.Unlock()
	metrics.PeerRecv.With(metrics.PeerIDLabel, c.remotePub.String()).Add(float64(len(m)))
	return nil
}

// ErrAlreadyClosed is an error for when `Close` is called on a closed connection.
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
	defer c.wmtx.Unlock()
	err := c.closeUnlocked()
	if err != nil {
		return err
	}
	close(c.stopSending)
	return nil
}

// Closed returns whether the connection is closed
func (c *FormattedConnection) Closed() bool {
	c.wmtx.Lock()
	defer c.wmtx.Unlock()
	return c.closed
}

var (
	// ErrTriedToSetupExistingConn occurs when handshake packet is sent twice on a connection
	ErrTriedToSetupExistingConn = errors.New("tried to setup existing connection")
	// ErrMsgExceededLimit occurs when a received message size exceeds the defined message size
	ErrMsgExceededLimit = errors.New("message size exceeded limit")
)

func (c *FormattedConnection) setupIncoming(timeout time.Duration) error {
	be := make(chan struct {
		b []byte
		e error
	})

	go func() {
		// TODO: some other way to make sure this groutine closes
		err := c.deadliner.SetReadDeadline(time.Now().Add(timeout))
		if err != nil {
			be <- struct {
				b []byte
				e error
			}{b: nil, e: err}
			return
		}
		msg, err := c.r.Next()
		be <- struct {
			b []byte
			e error
		}{b: msg, e: err}
		err = c.deadliner.SetReadDeadline(time.Time{}) // disable read deadline
		if err != nil {
			c.logger.Warning("could not set a read deadline err:", err)
		}
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
