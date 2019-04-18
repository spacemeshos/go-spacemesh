package net

import (
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"net"

	"github.com/spacemeshos/go-spacemesh/log"

	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

const maxMessageSize = 2048

// UDPMessageEvent is an event about a udp message. passed through a channel
type UDPMessageEvent struct {
	From     p2pcrypto.PublicKey
	FromAddr net.Addr
	Message  []byte
}

// UDPNet is used to listen on or send udp messages
type UDPNet struct {
	local      *node.LocalNode
	logger     log.Log
	udpAddress *net.UDPAddr
	config     config.Config
	msgChan    chan UDPMessageEvent
	conn       *net.UDPConn
	cache      *sessionCache
	shutdown   chan struct{}
}

// NewUDPNet creates a UDPNet. returns error if the listening can't be resolved
func NewUDPNet(config config.Config, local *node.LocalNode, log log.Log) (*UDPNet, error) {
	addr, err := net.ResolveUDPAddr("udp", local.Address())
	if err != nil {
		return nil, err
	}

	n := &UDPNet{
		local:      local,
		logger:     log,
		udpAddress: addr,
		config:     config,
		msgChan:    make(chan UDPMessageEvent, config.BufferSize),
		shutdown:   make(chan struct{}),
	}

	n.cache = newSessionCache(n.initSession)

	return n, nil
}

// Start will trigger listening on the configured port
func (n *UDPNet) Start() error {
	listener, err := newUDPListener(n.udpAddress)
	if err != nil {
		return err
	}
	n.conn = listener
	n.logger.Info("Started listening on udp:%v", listener.LocalAddr().String())
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
	err = listen.SetReadBuffer(maxMessageSize)
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

// Send writes a udp packet to the target with the given data
func (n *UDPNet) Send(to node.Node, data []byte) error {

	raddr, err := resolveUDPAddr(to.Address())
	if err != nil {
		return err
	}

	ns := n.cache.GetOrCreate(to.PublicKey())

	if err != nil {
		return err
	}

	sealed := ns.SealMessage(data)
	final := p2pcrypto.PrependPubkey(sealed, n.local.PublicKey())

	_, err = n.conn.WriteToUDP(final, raddr)

	return err
}

func resolveUDPAddr(addr string) (*net.UDPAddr, error) {
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	// TODO: only accept local (unspecified/loopback) IPs from other local ips.
	if raddr.IP.IsUnspecified() {
		if ip4 := raddr.IP.To4(); ip4 != nil {
			raddr.IP = IPv4LoopbackAddress
		} else if ip6 := raddr.IP.To16(); ip6 != nil {
			raddr.IP = net.IPv6loopback
		}
	}

	return raddr, nil
}

// IncomingMessages is a channel where incoming UDPMessagesEvents will stream
func (n *UDPNet) IncomingMessages() chan UDPMessageEvent {
	return n.msgChan
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

		// todo : check size?
		copybuf := make([]byte, size)
		copy(copybuf, buf)

		msg, pk, err := p2pcrypto.ExtractPubkey(copybuf)

		if err != nil {
			n.logger.Warning("error can't extract public key from udp message. (addr=%v), err=%v", addr.String(), err)
			continue
		}

		ns := n.cache.GetOrCreate(pk)

		if ns == nil {
			n.logger.Warning("coul'd not create session with %v:%v skipping message..", addr.String(), pk.String())
			continue
		}

		final, err := ns.OpenMessage(msg)
		if err != nil {
			n.logger.Warning("skipping udp with session message err=%v msg=", err, copybuf)
			// todo: remove malfunctioning session, ban ip ?
			continue
		}

		select {
		case n.msgChan <- UDPMessageEvent{pk, addr, final}:
		case <-n.shutdown:
			return
		}

	}
}
