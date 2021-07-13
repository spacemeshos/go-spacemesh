package net

import (
	"context"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"io"
	"net"
	"sync"
	"time"
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
	wmtx        sync.Mutex
	w           io.Writer
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
func newMsgConnection(conn readWriteCloseAddresser, netw networker,
	remotePub p2pcrypto.PublicKey, session NetworkSession, msgSizeLimit int, deadline time.Duration, log log.Log) msgConn {

	// todo parametrize channel size - hard-coded for now
	connection := &MsgConnection{
		logger:       log,
		id:           crypto.UUIDString(),
		created:      time.Now(),
		remotePub:    remotePub,
		remoteAddr:   conn.RemoteAddr(),
		r:            conn,
		w:            conn,
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
	//todo: re insert when log loss is fixed
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

			//todo: we are hiding the error here...
			if err := c.SendSock(m.payload); err != nil {
				log.With().Error("msgconnection: cannot send message to peer",
					log.String("peer_id", m.peerID),
					log.String("requestId", m.reqID),
					log.Err(err))
			}
		case <-c.stopSending:
			return
		}
	}
}

// Send pushes a message to the messages queue
func (c *MsgConnection) Send(ctx context.Context, m []byte) error {
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

	c.logger.WithContext(ctx).With().Debug("msgconnection: enqueuing outgoing message",
		log.Int("queue_length", len(c.messages)))
	if len(c.messages) > 30 {
		c.logger.WithContext(ctx).With().Warning("msgconnection: outbound send queue backlog",
			log.Int("queue_length", len(c.messages)))
	}
	c.messages <- msgToSend{m, reqID, peerID}
	return nil
}

// SendSock sends a message directly on the socket
func (c *MsgConnection) SendSock(m []byte) error {
	c.wmtx.Lock()
	if c.closed {
		c.wmtx.Unlock()
		return fmt.Errorf("connection was closed")
	}

	err := c.deadliner.SetWriteDeadline(time.Now().Add(c.deadline))
	if err != nil {
		return err
	}
	_, err = c.w.Write(m)
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

func (c *MsgConnection) closeUnlocked() error {
	if c.closed {
		return ErrAlreadyClosed
	}
	c.closed = true
	return nil
}

// Close closes the connection (implements io.Closer). It is go safe.
func (c *MsgConnection) Close() error {
	c.wmtx.Lock()
	defer c.wmtx.Unlock()
	if err := c.closeUnlocked(); err != nil {
		return err
	}
	close(c.stopSending)
	return nil
}

// Closed returns whether the connection is closed
func (c *MsgConnection) Closed() bool {
	c.wmtx.Lock()
	defer c.wmtx.Unlock()
	return c.closed
}

// Push outgoing message to the connections
// Read from the incoming new messages and send down the connection
func (c *MsgConnection) beginEventProcessing(ctx context.Context) {
	//TODO: use a buffer pool
	var err error
	for {
		buf := make([]byte, maxMessageSize)
		size, err := c.r.Read(buf)
		if err != nil && err != io.EOF {
			break
		}

		if c.session == nil {
			err = ErrTriedToSetupExistingConn
			break
		}

		if len(buf) > 0 {
			newbuf := make([]byte, size)
			copy(newbuf, buf[:size])

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
