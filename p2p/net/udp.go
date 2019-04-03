package net

import (
	"net"

	"github.com/spacemeshos/go-spacemesh/log"

	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

const maxMessageSize = 2048

// UDPMessageEvent is an event about a udp message. passed through a channel
type UDPMessageEvent struct {
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
	shutdown   chan struct{}
}

// NewUDPNet creates a UDPNet. returns error if the listening can't be resolved
func NewUDPNet(config config.Config, local *node.LocalNode, log log.Log) (*UDPNet, error) {
	addr, err := net.ResolveUDPAddr("udp", local.Address())
	if err != nil {
		return nil, err
	}

	return &UDPNet{
		local:      local,
		logger:     log,
		udpAddress: addr,
		config:     config,
		msgChan:    make(chan UDPMessageEvent, config.BufferSize),
		shutdown:   make(chan struct{}),
	}, nil
}

// Start will trigger listening on the configured port
func (n *UDPNet) Start() error {
	n.logger.Info("Starting to listen on udp:%v", n.udpAddress)
	listener, err := newUDPListener(n.udpAddress)
	if err != nil {
		return err
	}
	n.conn = listener
	go n.listenToUDPNetworkMessages(listener)

	return nil
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

// Send writes a udp packet to the target with the given data
func (n *UDPNet) Send(to node.Node, data []byte) error {
	// todo : handle session if not exist
	raddr, err := net.ResolveUDPAddr("udp", to.Address())
	if err != nil {
		return err
	}

	// TODO: only accept local (unspecified/loopback) IPs from other local ips.
	if raddr.IP.IsUnspecified() {
		if ip4 := raddr.IP.To4(); ip4 != nil {
			raddr.IP = IPv4LoopbackAddress
		} else if ip6 := raddr.IP.To16(); ip6 != nil {
			raddr.IP = net.IPv6loopback
		}
	}

	_, err = n.conn.WriteToUDP(data, raddr) // todo: use i to retransmit ?

	return err
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
		// todo: check if needs session before passing
		copybuf := make([]byte, size)
		copy(copybuf, buf)
		select {
		case n.msgChan <- UDPMessageEvent{addr, copybuf}:
		case <-n.shutdown:
			return
		}

	}
}
