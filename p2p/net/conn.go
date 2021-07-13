package net

import (
	"context"
	"errors"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/net/wire/delimited"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"

	"fmt"
	"io"
	"net"
	"sync"

	"github.com/spacemeshos/go-spacemesh/crypto"
)

var (
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

type msgToSend struct {
	payload []byte
	reqID   string
	peerID  string
}

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

	Send(ctx context.Context, m []byte) error
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
	messages    chan msgToSend
	stopSending chan struct{}
	close       io.Closer

	msgSizeLimit int
}

type networker interface {
	HandlePreSessionIncomingMessage(c Connection, msg []byte) error
	EnqueueMessage(ctx context.Context, ime IncomingMessageEvent)
	SubscribeClosingConnections(func(context.Context, ConnectionWithErr))
	publishClosingConnection(c ConnectionWithErr)
	NetworkID() uint32
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

type fmtConnection interface {
	Connection
	SendSock([]byte) error
	setupIncoming(context.Context, time.Duration) error
	beginEventProcessing(context.Context)
}

// Create a new connection wrapping a net.Conn with a provided connection manager
func newConnection(conn readWriteCloseAddresser, netw networker,
	remotePub p2pcrypto.PublicKey, session NetworkSession, msgSizeLimit int, deadline time.Duration, log log.Log) fmtConnection {

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
		messages:     make(chan msgToSend, MessageQueueSize),
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

func (c *FormattedConnection) publish(ctx context.Context, message []byte) {
	// Print a log line to establish a link between the originating sessionID and this requestID,
	// before the sessionID disappears.

	//todo: re insert if log loss is fixed
	//c.logger.WithContext(ctx).Debug("connection: enqueuing incoming message")

	// Rather than store the context on the heap, which is an antipattern, we instead extract the sessionID and store
	// that.
	ime := IncomingMessageEvent{Conn: c, Message: message}
	if requestID, ok := log.ExtractRequestID(ctx); ok {
		ime.RequestID = requestID
	}
	c.networker.EnqueueMessage(ctx, ime)
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
		case m := <-c.messages:
			c.logger.With().Debug("connection: sending outgoing message",
				log.String("peer_id", m.peerID),
				log.String("requestId", m.reqID),
				log.Int("queue_length", len(c.messages)))

			//todo: we are hiding the error here...
			if err := c.SendSock(m.payload); err != nil {
				c.logger.With().Error("connection: cannot send message to peer",
					log.String("peer_id", m.peerID),
					log.String("requestId", m.reqID),
					log.Err(err))
			}
		case <-c.stopSending:
			return
		}
	}
}

// Send pushes a message into the queue if the connection is not closed.
func (c *FormattedConnection) Send(ctx context.Context, m []byte) error {
	c.logger.WithContext(ctx).Debug("waiting for send lock")
	c.wmtx.Lock()
	if c.closed {
		c.wmtx.Unlock()
		return fmt.Errorf("connection was closed")
	}
	c.wmtx.Unlock()

	// extract some useful context
	reqID, _ := log.ExtractRequestID(ctx)
	peerID, _ := ctx.Value(log.PeerIDKey).(string)

	c.logger.WithContext(ctx).With().Debug("connection: enqueuing outgoing message",
		log.Int("queue_length", len(c.messages)))
	if len(c.messages) > 30 {
		c.logger.WithContext(ctx).With().Warning("connection: outbound send queue backlog",
			log.Int("queue_length", len(c.messages)))
	}
	c.messages <- msgToSend{m, reqID, peerID}
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

func (c *FormattedConnection) setupIncoming(ctx context.Context, timeout time.Duration) error {
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
			c.logger.With().Warning("could not set a read deadline", log.Err(err))
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
func (c *FormattedConnection) beginEventProcessing(ctx context.Context) {
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
			c.publish(log.WithNewRequestID(ctx), newbuf)
		}

		if err != nil {
			break
		}
	}

	if cerr := c.Close(); cerr != ErrAlreadyClosed {
		c.networker.publishClosingConnection(ConnectionWithErr{c, err})
	}
}
