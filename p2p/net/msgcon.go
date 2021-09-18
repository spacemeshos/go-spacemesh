package net

import (
	"context"
	"io"
	"net"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

// MsgConnection is an io.Writer and an io.Closer
// A network connection supporting full-duplex messaging
// It resembles the Connection interface but suits a packet oriented socket.
type MsgConnection struct {
	// metadata for logging / debugging
	logger      log.Log
	id          string // uuid for logging
	created     time.Time
	remotePub   p2pcrypto.PublicKey
	remoteAddr  net.Addr
	networker   networker // network context
	session     NetworkSession
	deadline    time.Duration
	r           io.Reader
	cmtx        sync.Mutex // protect closed status
	w           io.Writer
	closer      io.Closer
	closed      bool
	deadliner   deadliner
	messages    chan msgToSend
	stopSending chan struct{}

	msgSizeLimit int
}

type msgConn interface {
	Connection
	beginEventProcessing(context.Context)
}

// Create a new connection wrapping a net.Conn with a provided connection manager
func newMsgConnection(
	conn readWriteCloseAddresser,
	netw networker,
	remotePub p2pcrypto.PublicKey,
	session NetworkSession,
	msgSizeLimit int,
	deadline time.Duration,
	log log.Log) msgConn {
	messages := make(chan msgToSend, MessageQueueSize)
	connection := newMsgConnectionWithMessagesChan(conn, netw, remotePub, session, msgSizeLimit, deadline, messages, log)
	go connection.sendListener()
	return connection
}

func newMsgConnectionWithMessagesChan(
	conn readWriteCloseAddresser,
	netw networker,
	remotePub p2pcrypto.PublicKey,
	session NetworkSession,
	msgSizeLimit int,
	deadline time.Duration,
	messages chan msgToSend,
	log log.Log) *MsgConnection {
	// todo parametrize channel size - hard-coded for now
	return &MsgConnection{
		logger:       log,
		id:           crypto.UUIDString(),
		created:      time.Now(),
		remotePub:    remotePub,
		remoteAddr:   conn.RemoteAddr(),
		r:            conn,
		w:            conn,
		closer:       conn,
		deadline:     deadline,
		deadliner:    conn,
		networker:    netw,
		session:      session,
		messages:     messages,
		stopSending:  make(chan struct{}),
		msgSizeLimit: msgSizeLimit,
	}
}

// ID returns the channel's ID
func (c *MsgConnection) ID() string {
	return c.id
}

// RemoteAddr returns the channel's remote peer address
func (c *MsgConnection) RemoteAddr() net.Addr {
	return c.remoteAddr
}

// SetRemotePublicKey sets the remote peer's public key
func (c *MsgConnection) SetRemotePublicKey(key p2pcrypto.PublicKey) {
	c.remotePub = key
}

// RemotePublicKey returns the remote peer's public key
func (c *MsgConnection) RemotePublicKey() p2pcrypto.PublicKey {
	return c.remotePub
}

// SetSession sets the network session
func (c *MsgConnection) SetSession(session NetworkSession) {
	c.session = session
}

// Session returns the network session
func (c *MsgConnection) Session() NetworkSession {
	return c.session
}

// String returns a string describing the connection
func (c *MsgConnection) String() string {
	return c.id
}

// Created saves the time when the connection was created
func (c *MsgConnection) Created() time.Time {
	return c.created
}

func (c *MsgConnection) publish(ctx context.Context, message []byte) {
	// Print a log line to establish a link between the originating sessionID and this requestID,
	// before the sessionID disappears.
	// This causes issues with the p2p system test, but leaving here for debugging purposes.
	//c.logger.WithContext(ctx).Debug("msgconnection: enqueuing incoming message")

	// Rather than store the context on the heap, which is an antipattern, we instead extract the relevant IDs and
	// store those.
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

func (c *MsgConnection) sendListener() {
	for {
		select {
		case m := <-c.messages:
			c.logger.With().Debug("msgconnection: sending outgoing message",
				log.String("peer_id", m.peerID),
				log.String("requestId", m.reqID),
				log.Int("queue_length", len(c.messages)))

			// TODO: do we need to propagate this error upwards or add a callback?
			// see https://github.com/spacemeshos/go-spacemesh/issues/2733
			if err := c.sendSock(m.payload); err != nil {
				c.logger.With().Error("msgconnection: cannot send message to peer",
					log.String("peer_id", m.peerID),
					log.String("requestId", m.reqID),
					log.Err(err))
				return
			}
		case <-c.stopSending:
			return
		}
	}
}

// Send pushes a message to the messages queue
func (c *MsgConnection) Send(ctx context.Context, m []byte) error {
	c.cmtx.Lock()
	if c.closed {
		c.cmtx.Unlock()
		return ErrClosed
	}
	c.cmtx.Unlock()

	// extract some useful context
	reqID, _ := log.ExtractRequestID(ctx)
	peerID, _ := ctx.Value(log.PeerIDKey).(string)

	c.logger.WithContext(ctx).With().Debug("msgconnection: enqueuing outgoing message",
		log.Int("queue_length", len(c.messages)))
	if len(c.messages) > 30 {
		c.logger.WithContext(ctx).With().Warning("msgconnection: outbound send queue backlog",
			log.Int("queue_length", len(c.messages)))
	}

	// perform a non-blocking send and drop the peer if the channel is full
	// otherwise, the entire gossip stack will get blocked
	select {
	case c.messages <- msgToSend{m, reqID, peerID}:
		return nil
	case <-c.stopSending:
		return ErrClosed
	default:
		_ = c.Close()
		c.networker.publishClosingConnection(ConnectionWithErr{c, ErrQueueFull})
		return ErrQueueFull
	}
}

// sendSock sends a message directly on the socket
func (c *MsgConnection) sendSock(m []byte) error {
	if err := c.deadliner.SetWriteDeadline(time.Now().Add(c.deadline)); err != nil {
		return err
	}

	// the underlying net.Conn object performs its own write locking and is goroutine safe, so no need for a
	// mutex here. see https://github.com/spacemeshos/go-spacemesh/pull/2435#issuecomment-851039112.
	if _, err := c.w.Write(m); err != nil {
		if err := c.Close(); err != ErrAlreadyClosed {
			c.networker.publishClosingConnection(ConnectionWithErr{c, err}) // todo: reconsider
		}
		return err
	}
	metrics.PeerRecv.With(metrics.PeerIDLabel, c.remotePub.String()).Add(float64(len(m)))
	return nil
}

// Close closes the connection (implements io.Closer). It is go safe.
func (c *MsgConnection) Close() error {
	c.cmtx.Lock()
	defer c.cmtx.Unlock()
	if c.closed {
		return ErrAlreadyClosed
	}
	c.closed = true
	close(c.stopSending)
	return c.closer.Close()
}

// Closed returns whether the connection is closed
func (c *MsgConnection) Closed() bool {
	c.cmtx.Lock()
	defer c.cmtx.Unlock()
	return c.closed
}

// Push outgoing message to the connections
// Read from the incoming new messages and send down the connection
func (c *MsgConnection) beginEventProcessing(ctx context.Context) {
	var (
		err error
		buf = make([]byte, maxMessageSize)
		n   int
	)
	for {
		n, err = c.r.Read(buf)
		if err != nil && err != io.EOF {
			break
		}
		if c.session == nil {
			err = ErrTriedToSetupExistingConn
			break
		}
		if n > 0 {
			newbuf := make([]byte, n)
			copy(newbuf, buf)

			// Create a new requestId for context
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
