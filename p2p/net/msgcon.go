package net

import (
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

// FormattedConnection is an io.Writer and an io.Closer
// A network connection supporting full-duplex messaging
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
	timeout     time.Duration
	r           io.Reader
	wmtx        sync.Mutex
	w           io.Writer
	closed      bool
	deadliner   deadliner
	messages    chan []byte
	stopSending chan struct{}
	close       io.Closer

	msgSizeLimit int
}

// Create a new connection wrapping a net.Conn with a provided connection manager
func newMsgConnection(conn readWriteCloseAddresser, netw networker,
	remotePub p2pcrypto.PublicKey, session NetworkSession, msgSizeLimit int, deadline time.Duration, log log.Log) *MsgConnection {

	// todo parametrize channel size - hard-coded for now
	connection := &MsgConnection{
		logger:       log,
		id:           crypto.UUIDString(),
		created:      time.Now(),
		remotePub:    remotePub,
		remoteAddr:   conn.RemoteAddr(),
		r:            conn,
		w:            conn,
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

func (c *MsgConnection) Created() time.Time {
	return c.created
}

func (c *MsgConnection) publish(message []byte) {
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

func (c *MsgConnection) sendListener() {
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

func (c *MsgConnection) Send(m []byte) error {
	c.wmtx.Lock()
	if c.closed {
		c.wmtx.Unlock()
		return fmt.Errorf("connection was closed")
	}
	c.wmtx.Unlock()
	c.messages <- m
	return nil
}

func (c *MsgConnection) SendSock(m []byte) error {
	c.wmtx.Lock()
	if c.closed {
		c.wmtx.Unlock()
		return fmt.Errorf("connection was closed")
	}

	c.deadliner.SetWriteDeadline(time.Now().Add(c.deadline))
	_, err := c.w.Write(m)
	if err != nil {
		cerr := c.closeUnlocked()
		c.wmtx.Unlock()
		if cerr != ErrAlreadyClosed {
			c.networker.publishClosingConnection(ConnectionWithErr{c, err}) // todo: reconsider
		}
		return err
	}
	c.wmtx.Unlock()
	metrics.PeerRecv.With(metrics.PeerIdLabel, c.remotePub.String()).Add(float64(len(m)))
	return nil
}

func (c *MsgConnection) closeUnlocked() error {
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
func (c *MsgConnection) Close() error {
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
func (c *MsgConnection) Closed() bool {
	c.wmtx.Lock()
	defer c.wmtx.Unlock()
	return c.closed
}

// Push outgoing message to the connections
// Read from the incoming new messages and send down the connection
func (c *MsgConnection) beginEventProcessing() {
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
