package net

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

// TODO: we should remove this const. Should  not depend on the number of addresses.
// TODO: the number of addresses should be derived from the limit provided in the config
const maxMessageSize = 30000 // @see getAddrMax
const maxUDPConn = 2048
const maxUDPLife = time.Hour * 24

// UDPMessageEvent is an event about a udp message. passed through a channel
type UDPMessageEvent struct {
	From     p2pcrypto.PublicKey
	FromAddr net.Addr
	Message  []byte
}

// UDPListener is the api required for listening on udp messages.
type UDPListener interface {
	LocalAddr() net.Addr
	Close() error
	WriteToUDP(final []byte, addr *net.UDPAddr) (int, error)
	ReadFrom(p []byte) (n int, addr net.Addr, err error)
	WriteTo(p []byte, addr net.Addr) (n int, err error)
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

type connWrapper struct {
	uConn udpConn
	mConn msgConn
}

func (cw connWrapper) Close() error {
	err1 := cw.mConn.Close()
	err2 := cw.uConn.Close()
	if err1 != nil || err2 != nil {
		return fmt.Errorf("errors closing wrapped connections: %v %v", err1, err2)
	}
	return nil
}

// UDPNet is used to listen on or send udp messages
type UDPNet struct {
	local   node.LocalNode
	logger  log.Log
	config  config.Config
	msgChan chan IncomingMessageEvent
	conn    UDPListener
	cache   *sessionCache

	regMutex         sync.RWMutex
	regNewRemoteConn []func(NewConnectionEvent)

	clsMutex           sync.RWMutex
	closingConnections []func(context.Context, ConnectionWithErr)

	incomingConn map[string]connWrapper

	shutdownCtx context.Context
}

// NewUDPNet creates a UDPNet
func NewUDPNet(ctx context.Context, config config.Config, localEntity node.LocalNode, log log.Log) (*UDPNet, error) {
	n := &UDPNet{
		local:        localEntity,
		logger:       log,
		config:       config,
		msgChan:      make(chan IncomingMessageEvent, config.BufferSize),
		incomingConn: make(map[string]connWrapper, maxUDPConn),
		shutdownCtx:  ctx,
	}

	n.cache = newSessionCache(n.initSession)

	return n, nil
}

// SubscribeClosingConnections registers a callback for a new connection event. all registered callbacks are called before moving.
func (n *UDPNet) SubscribeClosingConnections(f func(ctx context.Context, connection ConnectionWithErr)) {
	n.clsMutex.Lock()
	n.closingConnections = append(n.closingConnections, f)
	n.clsMutex.Unlock()
}

func (n *UDPNet) publishClosingConnection(connection ConnectionWithErr) {
	n.clsMutex.RLock()
	for _, f := range n.closingConnections {
		f(context.TODO(), connection)
	}
	n.clsMutex.RUnlock()
}

// SubscribeOnNewRemoteConnections registers a callback for a new connection event. all registered callbacks are called before moving.
func (n *UDPNet) SubscribeOnNewRemoteConnections(f func(event NewConnectionEvent)) {
	n.regMutex.Lock()
	n.regNewRemoteConn = append(n.regNewRemoteConn, f)
	n.regMutex.Unlock()
}

func (n *UDPNet) publishNewRemoteConnectionEvent(conn Connection, node *node.Info) {
	n.regMutex.RLock()
	for _, f := range n.regNewRemoteConn {
		f(NewConnectionEvent{conn, node})
	}
	n.regMutex.RUnlock()
}

// Start will start reading messages from the udp socket and pass them up the channels
func (n *UDPNet) Start(ctx context.Context, listener UDPListener) {
	n.conn = listener
	n.logger.WithContext(ctx).With().Info("started udp server listening for messages",
		log.String("udpaddr", listener.LocalAddr().String()))
	go n.listenToUDPNetworkMessages(ctx, listener)
}

// LocalAddr returns the local listening addr, will panic before running Start. or if start errored
func (n *UDPNet) LocalAddr() net.Addr {
	return n.conn.LocalAddr()
}

// Shutdown stops listening and closes the connection
func (n *UDPNet) Shutdown() {
	if n.conn != nil {
		n.conn.Close()
	}
	for _, conn := range n.incomingConn {
		conn.Close()
	}
}

// IPv4LoopbackAddress is a local IPv4 loopback
var IPv4LoopbackAddress = net.IP{127, 0, 0, 1}

func (n *UDPNet) initSession(remote p2pcrypto.PublicKey) NetworkSession {
	session := createSession(n.local.PrivateKey(), remote)
	return session
}

// NodeAddr makes a UDPAddr from a Info struct
func NodeAddr(info *node.Info) *net.UDPAddr {
	return &net.UDPAddr{IP: info.IP, Port: int(info.DiscoveryPort)}
}

// Send writes a udp packet to the target with the given data
func (n *UDPNet) Send(to *node.Info, data []byte) error {
	ns := n.cache.GetOrCreate(to.PublicKey())
	sealed := ns.SealMessage(data)
	final := p2pcrypto.PrependPubkey(sealed, n.local.PublicKey())
	addr := NodeAddr(to)
	_, err := n.conn.WriteToUDP(final, addr)
	return err
}

// IncomingMessages is a channel where incoming UDPMessagesEvents will stream
func (n *UDPNet) IncomingMessages() chan IncomingMessageEvent {
	return n.msgChan
}

// Dial creates a Connection interface which is wrapped around a udp socket with a session and start listening on messages from it.
// it uses the `connect` syscall
func (n *UDPNet) Dial(ctx context.Context, address net.Addr, remotePublicKey p2pcrypto.PublicKey) (Connection, error) {
	udpcon, err := net.DialUDP("udp", nil, address.(*net.UDPAddr))
	if err != nil {
		return nil, err
	}

	ns := n.cache.GetOrCreate(remotePublicKey)

	conn := newMsgConnection(udpcon, n, remotePublicKey, ns, n.config.MsgSizeLimit, n.config.ResponseTimeout, n.logger)

	// The connection is automatically closed when there's nothing more to read, no need to close it here
	go conn.beginEventProcessing(log.WithNewSessionID(ctx, log.String("netproto", "udp")))
	return conn, nil
}

// HandlePreSessionIncomingMessage is used to satisfy an api similar to the tcpnet. not used here.
func (n *UDPNet) HandlePreSessionIncomingMessage(c Connection, msg []byte) error {
	return errors.New("not implemented")
}

// EnqueueMessage pushes an incoming message event into the queue
func (n *UDPNet) EnqueueMessage(ctx context.Context, ime IncomingMessageEvent) {
	select {
	case n.msgChan <- ime:
		n.logger.WithContext(ctx).With().Debug("recv udp message",
			log.String("from", ime.Conn.RemotePublicKey().String()),
			log.String("fromaddr", ime.Conn.RemoteAddr().String()),
			log.Int("len", len(ime.Message)))
	case <-n.shutdownCtx.Done():
		return
	}
}

// NetworkID returns the network id given and used for creating a session.
func (n *UDPNet) NetworkID() uint32 {
	return 0
}

// main listening loop
func (n *UDPNet) listenToUDPNetworkMessages(ctx context.Context, listener net.PacketConn) {
	n.logger.Info("listening for incoming udp connections")

	buf := make([]byte, maxMessageSize) // todo: buffer pool ?
	for {
		size, addr, err := listener.ReadFrom(buf)
		if err != nil {
			if temp, ok := err.(interface {
				Temporary() bool
			}); ok && temp.Temporary() {
				n.logger.With().Warning("temporary udp error", log.Err(err))
				continue
			} else {
				n.logger.With().Error("listen udp error, stopping server", log.Err(err))
				return
			}
		}

		if n.config.MsgSizeLimit != config.UnlimitedMsgSize && size > n.config.MsgSizeLimit {
			n.logger.With().Error("listenToUDPNetworkMessages: message is too big",
				log.Int("limit", n.config.MsgSizeLimit),
				log.Int("actual", size))
			continue
		}

		copybuf := make([]byte, size)
		copy(copybuf, buf)

		conn, err := n.getConn(addr)
		if err != nil {
			n.logger.Debug("creating new connection")
			_, pk, err := p2pcrypto.ExtractPubkey(copybuf)

			if err != nil {
				n.logger.With().Warning("error can't extract public key from udp message",
					log.String("addr", addr.String()),
					log.Err(err))
				continue
			}

			ns := n.cache.GetOrCreate(pk)

			if ns == nil {
				n.logger.With().Warning("could not create session, skipping message",
					log.String("addr", addr.String()),
					log.String("pk", pk.String()))
				continue
			}

			host, port, err := net.SplitHostPort(addr.String())
			if err != nil {
				n.logger.With().Warning("could not parse address, skipping message",
					log.String("addr", addr.String()),
					log.String("pk", pk.String()))
				continue
			}

			iport, err := strconv.Atoi(port)
			if err != nil {
				n.logger.With().Warning("failed converting port to int", log.String("port", port))
				continue
			}

			conn = &udpConnWrapper{
				created:   time.Now(),
				incChan:   make(chan []byte, 1000),
				closeChan: make(chan struct{}, 1),
				conn:      n.conn,
				remote:    addr,
			}

			mconn := newMsgConnection(conn, n, pk, ns, n.config.MsgSizeLimit, n.config.DialTimeout, n.logger)
			n.publishNewRemoteConnectionEvent(mconn, node.NewNode(pk, net.ParseIP(host), 0, uint16(iport)))
			n.addConn(addr, conn, mconn)

			// mconn will be closed when the connection is evicted (or at shutdown)
			go mconn.beginEventProcessing(context.TODO())
		}

		if err := conn.PushIncoming(copybuf); err != nil {
			n.logger.With().Warning("error pushing incoming message to connection",
				log.String("remote_addr", conn.RemoteAddr().String()))
		}
	}
}

func (n *UDPNet) addConn(addr net.Addr, ucw udpConn, conn msgConn) {
	evicted := false
	lastk := ""
	if len(n.incomingConn) >= maxUDPConn {
		n.logger.Debug("udp connection cache is full, evicting one session")
		for k, c := range n.incomingConn {
			lastk = k
			if time.Since(c.uConn.Created()) > maxUDPLife {
				delete(n.incomingConn, k)
				if err := c.Close(); err != nil {
					n.logger.With().Warning("error closing udp socket", log.Err(err))
				}
				evicted = true
				break
			}
		}

		if !evicted {
			if err := n.incomingConn[lastk].Close(); err != nil {
				n.logger.With().Warning("error closing udp socket", log.Err(err))
			}
			delete(n.incomingConn, lastk)
		}
	}

	// Wrap the connection objects together to make sure they're both closed
	n.incomingConn[addr.String()] = connWrapper{ucw, conn}
}

func (n *UDPNet) getConn(addr net.Addr) (udpConn, error) {
	if c, ok := n.incomingConn[addr.String()]; ok {
		if time.Since(c.uConn.Created()) > maxUDPLife {
			if err := c.Close(); err != nil {
				n.logger.With().Warning("error closing udp socket", log.Err(err))
			}
			delete(n.incomingConn, addr.String())
			return nil, errors.New("expired")
		}
		return c.uConn, nil
	}
	return nil, errors.New("does not exist")
}

type udpConn interface {
	net.Conn
	Created() time.Time
	PushIncoming(b []byte) error
}

type udpConnWrapper struct {
	created   time.Time
	incChan   chan []byte
	closeChan chan struct{}

	rDeadline time.Time
	wDeadline time.Time

	conn   UDPListener
	remote net.Addr
}

func (ucw *udpConnWrapper) PushIncoming(b []byte) error {
	select {
	case ucw.incChan <- b:
		break
	case <-ucw.closeChan:
		return errors.New("closed")
	}
	return nil
}

func (ucw *udpConnWrapper) SetDeadline(t time.Time) error {
	ucw.rDeadline = t
	ucw.wDeadline = t
	return nil
}

func (ucw *udpConnWrapper) SetReadDeadline(t time.Time) error {
	ucw.rDeadline = t
	return nil
}

func (ucw *udpConnWrapper) SetWriteDeadline(t time.Time) error {
	ucw.wDeadline = t
	return nil
}

func (ucw *udpConnWrapper) Created() time.Time {
	return ucw.created
}

func (ucw *udpConnWrapper) LocalAddr() net.Addr {
	return ucw.conn.LocalAddr()
}

func (ucw *udpConnWrapper) RemoteAddr() net.Addr {
	return ucw.remote
}

func (ucw *udpConnWrapper) Read(b []byte) (int, error) {
	select {
	case msg := <-ucw.incChan:
		copy(b, msg)
		log.Debug("passing message to conn")
		return len(msg), nil
	case <-ucw.closeChan:
		return 0, errors.New("closed")
	}
}

func (ucw *udpConnWrapper) Write(b []byte) (int, error) {
	return ucw.conn.WriteTo(b, ucw.remote)
}

func (ucw *udpConnWrapper) Close() error {
	close(ucw.closeChan)
	return nil
}
