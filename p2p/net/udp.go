package net

import (
	"net"

	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

const maxMessageSize = 2048

type UDPMessageEvent struct {
	Size    int
	Addr    net.Addr
	Message []byte
}

type UDPNet struct {
	local      *node.LocalNode
	udpAddress *net.UDPAddr
	config     config.Config
	msgChan    chan UDPMessageEvent
	conn       *net.UDPConn
	shutdown   chan struct{}
}

func NewUDPNet(config config.Config, local *node.LocalNode) (*UDPNet, error) {
	addr, err := net.ResolveUDPAddr("udp", local.Address())
	if err != nil {
		return nil, err
	}

	return &UDPNet{
		local:      local,
		udpAddress: addr,
		config:     config,
		msgChan:    make(chan UDPMessageEvent, config.BufferSize),
		shutdown:   make(chan struct{}),
	}, nil
}

func (n *UDPNet) Start() error {
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
	n.conn.Close()
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

func (n *UDPNet) Send(to node.Node, data []byte) error {
	// todo : handle session if not exist
	addr, err := net.ResolveUDPAddr("udp", to.Address())
	if err != nil {
		return err
	}
	_, err = n.conn.WriteToUDP(data, addr) // todo: use i to retransmit ?

	return err
}

func (n *UDPNet) IncomingMessages() chan UDPMessageEvent {
	return n.msgChan
}

func (n *UDPNet) listenToUDPNetworkMessages(listener net.PacketConn) {
	for {
		buf := make([]byte, maxMessageSize) // todo: buffer pool ?
		size, addr, err := listener.ReadFrom(buf)
		if err != nil {
			if temp, ok := err.(interface {
				Temporary() bool
			}); ok && temp.Temporary() {
				//n.logger.With().Warning("Temporary UDP error", log.Err(err))
				continue
			} else {
				//n.logger.With().Error("Listen UDP error, stopping server", log.Err(err))
				return
			}
		}

		go func(msg UDPMessageEvent) {

			select {
			case n.msgChan <- msg:
			case <-n.shutdown:
				return
			}

		}(UDPMessageEvent{size, addr, buf})

	}
}
