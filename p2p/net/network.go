package net

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"net"
	"time"
)

// Net is a connection manager able to dial remote endpoints
// Net clients should register all callbacks
// Connections may be initiated by DialTCP() or by remote clients connecting to the listen address
// ConnManager includes a TCP server, and a TCP client
// It provides full duplex messaging functionality over the same tcp/ip connection
// Network should not know about higher-level networking types such as remoteNode, swarm and networkSession
// Network main client is the swarm
// Net has no channel events processing loops - clients are responsible for polling these channels and popping events from them
type Net interface {
	DialTCP(address string, timeOut time.Duration, keepAlive time.Duration) (Connection, error) // Connect to a remote node. Can send when no error.
	GetNewConnections() chan Connection
	GetClosingConnections() chan Connection
	GetConnectionErrors() chan ConnectionError
	GetIncomingMessage() chan IncomingMessage
	GetMessageSendErrors() chan MessageSendError
	GetMessageSentCallback() chan MessageSentEvent
	Shutdown()
}

// impl internal type
type netImpl struct {
	tcpListener      net.Listener
	tcpListenAddress string // Address to open connection: localhost:9999

	newConnections     chan Connection
	closingConnections chan Connection
	connectionErrors   chan ConnectionError
	incomingMessages   chan IncomingMessage
	messageSendErrors  chan MessageSendError

	messageSentEvents chan MessageSentEvent

	isShuttingDown bool
}

// NewNet creates a new network.
// It attempts to tcp listen on address. e.g. localhost:1234 .
func NewNet(tcpListenAddress string, config nodeconfig.Config) (Net, error) {

	n := &netImpl{
		tcpListenAddress:   tcpListenAddress,
		newConnections:     make(chan Connection, 20),
		closingConnections: make(chan Connection, 20),
		connectionErrors:   make(chan ConnectionError, 20),
		incomingMessages:   make(chan IncomingMessage, 20),
		messageSendErrors:  make(chan MessageSendError, 20),
		messageSentEvents:  make(chan MessageSentEvent, 20),
	}

	err := n.listen()

	if err != nil {
		return nil, err
	}

	log.Info("created network with tcp address: %s", tcpListenAddress)

	return n, nil
}

func (n *netImpl) GetMessageSentCallback() chan MessageSentEvent {
	return n.messageSentEvents
}

// GetNewConnections Implement Network interface public channel accessors.
func (n *netImpl) GetNewConnections() chan Connection {
	return n.newConnections
}

func (n *netImpl) GetClosingConnections() chan Connection {
	return n.closingConnections
}

func (n *netImpl) GetConnectionErrors() chan ConnectionError {
	return n.connectionErrors
}

func (n *netImpl) GetIncomingMessage() chan IncomingMessage {
	return n.incomingMessages
}

func (n *netImpl) GetMessageSendErrors() chan MessageSendError {
	return n.messageSendErrors
}

// Dial a remote server with provided time out
// address:: ip:port
// Returns established connection that local clients can send messages to or error if failed
// to establish a connection
func (n *netImpl) DialTCP(address string, timeOut time.Duration, keepAlive time.Duration) (Connection, error) {
	if n.isShuttingDown {
		return nil, fmt.Errorf("can't dial because the connection is shutting down")
	}
	// connect via dialer so we can set tcp network params
	dialer := &net.Dialer{}
	dialer.KeepAlive = keepAlive // drop connections after a period of inactivity
	dialer.Timeout = timeOut     // max time bef

	log.Info("TCP dialing %s ...", address)

	netConn, err := dialer.Dial("tcp", address)

	if err != nil {
		return nil, err
	}

	log.Info("Connected to %s...", address)
	c := newConnection(netConn, n, Local)
	return c, nil
}

func (n *netImpl) Shutdown() {
	n.isShuttingDown = true
	n.tcpListener.Close()
}

// Start network server
func (n *netImpl) listen() error {
	log.Info("Starting to listen...")
	tcpListener, err := net.Listen("tcp", n.tcpListenAddress)
	if err != nil {
		return err
	}
	n.tcpListener = tcpListener
	go n.acceptTCP()
	return nil
}

func (n *netImpl) acceptTCP() {
	for {
		log.Info("Waiting for incoming connections...")
		netConn, err := n.tcpListener.Accept()
		if err != nil {

			if !n.isShuttingDown {
				log.Error("Failed to accept connection request", err)
			}
			return
		}

		log.Info("Got new connection... Remote Address: %s", netConn.RemoteAddr())
		c := newConnection(netConn, n, Remote)
		n.newConnections <- c
	}
}
