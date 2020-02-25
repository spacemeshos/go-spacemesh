package net

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"net"
	"strconv"
	"sync"
	"time"
)

// TODO: we should remove this const. Should  not depend on the number of addresses.
// TODO: the number of addresses should be derived from the limit provided in the config
const maxMessageSize = 30000 // @see getAddrMax
const maxUDPConn = 2048
const maxUDPLife = time.Duration(time.Hour * 24)

// UDPMessageEvent is an event about a udp message. passed through a channel
type UDPMessageEvent struct {
	From     p2pcrypto.PublicKey
	FromAddr net.Addr
	Message  []byte
}

// UDPNet is used to listen on or send udp messages
type UDPNet struct {
	local      node.LocalNode
	logger     log.Log
	udpAddress *net.UDPAddr
	config     config.Config
	msgChan    chan IncomingMessageEvent
	conn       *net.UDPConn
	cache      *sessionCache

	regMutex         sync.RWMutex
	regNewRemoteConn []func(NewConnectionEvent)

	clsMutex           sync.RWMutex
	closingConnections []func(ConnectionWithErr)

	incomingConn map[string]udpConn

	shutdown chan struct{}
}

// NewUDPNet creates a UDPNet. returns error if the listening can't be resolved
func NewUDPNet(config config.Config, localEntity node.LocalNode, addr *net.UDPAddr, log log.Log) (*UDPNet, error) {
	n := &UDPNet{
		local:        localEntity,
		logger:       log,
		udpAddress:   addr,
		config:       config,
		msgChan:      make(chan IncomingMessageEvent, config.BufferSize),
		incomingConn: make(map[string]udpConn, maxUDPConn),
		shutdown:     make(chan struct{}),
	}

	n.cache = newSessionCache(n.initSession)

	return n, nil
}

// SubscribeClosingConnections registers a callback for a new connection event. all registered callbacks are called before moving.
func (n *UDPNet) SubscribeClosingConnections(f func(connection ConnectionWithErr)) {
	n.clsMutex.Lock()
	n.closingConnections = append(n.closingConnections, f)
	n.clsMutex.Unlock()
}

func (n *UDPNet) publishClosingConnection(connection ConnectionWithErr) {
	n.clsMutex.RLock()
	for _, f := range n.closingConnections {
		f(connection)
	}
	n.clsMutex.RUnlock()
}

// SubscribeOnNewRemoteConnections registers a callback for a new connection event. all registered callbacks are called before moving.
func (n *UDPNet) SubscribeOnNewRemoteConnections(f func(event NewConnectionEvent)) {
	n.regMutex.Lock()
	n.regNewRemoteConn = append(n.regNewRemoteConn, f)
	n.regMutex.Unlock()
}

func (n *UDPNet) publishNewRemoteConnectionEvent(conn Connection, node *node.NodeInfo) {
	n.regMutex.RLock()
	for _, f := range n.regNewRemoteConn {
		f(NewConnectionEvent{conn, node})
	}
	n.regMutex.RUnlock()
}

// Start will trigger listening on the configured port
func (n *UDPNet) Start() error {
	listener, err := newUDPListener(n.udpAddress)
	if err != nil {
		return err
	}
	n.conn = listener
	n.logger.Info("Started UDP server listening for messages on udp:%v", listener.LocalAddr().String())
	go n.listenToUDPNetworkMessages(listener)

	return nil
}

// LocalAddr returns the local listening addr, will panic before running Start. or if start errored
func (n *UDPNet) LocalAddr() net.Addr {
	return n.conn.LocalAddr()
}

// Shutdown stops listening and closes the connection
func (n *UDPNet) Shutdown() {
	close(n.shutdown)
	if n.conn != nil {
		n.conn.Close()
	}
}

func newUDPListener(listenAddress *net.UDPAddr) (*net.UDPConn, error) {
	//todo: grab different udp port from config
	listen, err := net.ListenUDP("udp", listenAddress)
	if err != nil {
		return nil, err
	}
	return listen, nil
}

var IPv4LoopbackAddress = net.IP{127, 0, 0, 1}

func (n *UDPNet) initSession(remote p2pcrypto.PublicKey) NetworkSession {
	session := createSession(n.local.PrivateKey(), remote)
	return session
}

func NodeAddr(info *node.NodeInfo) *net.UDPAddr {
	return &net.UDPAddr{IP: info.IP, Port: int(info.DiscoveryPort)}
}

// Send writes a udp packet to the target with the given data
func (n *UDPNet) Send(to *node.NodeInfo, data []byte) error {

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

func (n *UDPNet) Dial(address net.Addr, remotePublicKey p2pcrypto.PublicKey) (Connection, error) {
	udpcon, err := net.DialUDP("udp", nil, address.(*net.UDPAddr))
	if err != nil {
		return nil, err
	}

	ns := n.cache.GetOrCreate(remotePublicKey)

	conn := newMsgConnection(udpcon, n, remotePublicKey, ns, n.config.MsgSizeLimit, n.config.ResponseTimeout, n.logger)
	go conn.beginEventProcessing()
	return conn, nil
}

func (n *UDPNet) HandlePreSessionIncomingMessage(c Connection, msg []byte) error {
	return errors.New("not implemented")
}

func (n *UDPNet) EnqueueMessage(ime IncomingMessageEvent) {
	select {
	case n.msgChan <- ime:
		n.logger.With().Debug("recv udp message", log.String("from", ime.Conn.RemotePublicKey().String()), log.String("fromaddr", ime.Conn.RemoteAddr().String()), log.Int("len", len(ime.Message)))
	case <-n.shutdown:
		return
	}
}

func (n *UDPNet) NetworkID() int8 {
	return 0
}

// main listening loop
func (n *UDPNet) listenToUDPNetworkMessages(listener net.PacketConn) {
	buf := make([]byte, maxMessageSize) // todo: buffer pool ?
	for {
		size, addr, err := listener.ReadFrom(buf)
		if err != nil {
			if temp, ok := err.(interface {
				Temporary() bool
			}); ok && temp.Temporary() {
				n.logger.Warning("Temporary UDP error", err)
				continue
			} else {
				n.logger.With().Error("Listen UDP error, stopping server", log.Err(err))
				return
			}

		}

		if n.config.MsgSizeLimit != config.UnlimitedMsgSize && size > n.config.MsgSizeLimit {
			n.logger.With().Error("listenToUDPNetworkMessages: message is too big",
				log.Int("limit", n.config.MsgSizeLimit), log.Int("actual", size))
			continue
		}

		copybuf := make([]byte, size)
		copy(copybuf, buf)

		conn, ok := n.incomingConn[addr.String()]
		if !ok {
			n.logger.Debug("Creating new connection ")
			_, pk, err := p2pcrypto.ExtractPubkey(copybuf)

			if err != nil {
				n.logger.Warning("error can't extract public key from udp message. (Addr=%v), err=%v", addr.String(), err)
				continue
			}

			ns := n.cache.GetOrCreate(pk)

			if ns == nil {
				n.logger.Warning("could not create session with %v:%v skipping message..", addr.String(), pk.String())
				continue
			}

			host, port, err := net.SplitHostPort(addr.String())
			if err != nil {
				n.logger.Warning("could not parse address skipping message from  %v %s", addr.String(), pk)
				continue
			}

			iport, err := strconv.Atoi(port)
			if err != nil {
				n.logger.Warning("failed converting port to int %v", port)
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
			n.addConn(addr, conn)
			go mconn.beginEventProcessing()
		}

		err = conn.PushIncoming(copybuf)
		if err != nil {
			n.logger.Warning("error pushing incoming message to conn with %v", conn.RemoteAddr())
		}

	}
}

func (n *UDPNet) addConn(addr net.Addr, ucw udpConn) {
	evicted := false
	lastk := ""
	if len(n.incomingConn) >= maxUDPConn {
		for k, c := range n.incomingConn {
			lastk = k
			if time.Since(c.Created()) > maxUDPLife {
				delete(n.incomingConn, k)
				evicted = true
				n.incomingConn[k].Close()
				break
			}

		}

		if !evicted {
			n.incomingConn[lastk].Close()
			delete(n.incomingConn, lastk)
		}
	}
	n.incomingConn[addr.String()] = ucw
}

func (n *UDPNet) getConn(addr net.Addr) (udpConn, error) {
	if c, ok := n.incomingConn[addr.String()]; ok {
		if time.Since(c.Created()) > maxUDPLife {
			c.Close()
			delete(n.incomingConn, addr.String())
			return nil, errors.New("expired")
		}
		return c, nil
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

	conn   *net.UDPConn
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
		log.Debug("passing message to conn ")
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
