package net

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"gopkg.in/op/go-logging.v1"
	"net"
	"time"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/gogo/protobuf/proto"
	"errors"
	"github.com/spacemeshos/go-spacemesh/p2p/delimited"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"encoding/hex"
)

// There are two types of TCP connections
// 	TcpSecured - A session was established, all messages are sent encrypted
// 	TcpUnsecured - No session was established, messages are sent raw.
type ConnectionType int
const (
	TcpSecured ConnectionType = iota
	TcpUnsecured
)

type localNode interface {
	PublicKey() crypto.PublicKey
	PrivateKey() crypto.PrivateKey
}

type remoteNode interface {
	PublicKey() crypto.PublicKey
}

// Net is a connection manager able to dial remote endpoints
// Net clients should register all callbacks
// Connections may be initiated by Dial() or by remote clients connecting to the listen address
// ConnManager includes a TCP server, and a TCP client
// It provides full duplex messaging functionality over the same tcp/ip connection
// Network should not know about higher-level networking types such as remoteNode, swarm and networkSession
// Network main client is the swarm
// Net has no channel events processing loops - clients are responsible for polling these channels and popping events from them
type Net interface {
	Dial(address string, timeOut time.Duration, keepAlive time.Duration) (Connection, error) // Connect to a remote node. Can send when no error.
	GetNewConnections() chan Connection
	GetClosingConnections() chan Connection
	GetConnectionErrors() chan ConnectionError
	GetIncomingMessage() chan IncomingMessage
	GetMessageSendErrors() chan MessageSendError
	GetMessageSentCallback() chan MessageSentEvent
	GetLogger() *logging.Logger
	Shutdown()
}

// impl internal type
type netImpl struct {
	localNode localNode
	logger    *logging.Logger

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
func NewNet(tcpListenAddress string, config nodeconfig.Config, logger *logging.Logger, localEntity localNode) (Net, error) {

	n := &netImpl{
		localNode:			localEntity,
		logger:             logger,
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

	n.logger.Debug("created network with tcp address: %s", tcpListenAddress)

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

func (n *netImpl) GetLogger() *logging.Logger {
	return n.logger
}

func (n *netImpl) createConnection(address string, timeOut time.Duration, keepAlive time.Duration) (*Connection, error) {
	if n.isShuttingDown {
		return nil, fmt.Errorf("can't dial because the connection is shutting down")
	}
	// connect via dialer so we can set tcp network params
	dialer := &net.Dialer{}
	dialer.KeepAlive = keepAlive // drop connections after a period of inactivity
	dialer.Timeout = timeOut     // max time bef

	n.logger.Debug("TCP dialing %s ...", address)

	netConn, err := dialer.Dial("tcp", address)

	if err != nil {
		return nil, err
	}

	n.logger.Debug("Connected to %s...", address)
	c := newConnection(netConn, n, Local)

	// update on new connection
	n.newConnections <- c

	return c, nil
}

func (n *netImpl) createSecuredConnection(address string, remotePublicKey crypto.PublicKey, networkId int32, timeOut time.Duration, keepAlive time.Duration) (*Connection, error) {
	errMsg := "failed to establish secured connection."
	conn, err := n.createConnection(address, timeOut, keepAlive)
	if err != nil {
		return nil, err
	}
	data, session, err := p2p.GenerateHandshakeRequestData(n.localNode.PublicKey(), n.localNode.PrivateKey(), remotePublicKey, networkId)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("%s err: %v", errMsg, err)
	}
	n.logger.Debug("Creating session handshake request session id: %s", session.String())
	payload, err := proto.Marshal(data)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("%s err: %v", errMsg, err)
	}
	conn.Send(payload, session.ID())

	var msg delimited.MsgAndID
	var ok bool
	select {
	case msg, ok = <-conn.incomingMsgs.MsgChan:
		if !ok { // chan closed
		conn.Close()
		return nil, errors.New("%s err: incoming channel was closed unexpectedly")
		}
	case err := <-conn.incomingMsgs.ErrChan:
		return nil, fmt.Errorf("%s err: %v", errMsg, err)
	}

	respData := &pb.HandshakeData{}
	err = proto.Unmarshal(msg.Msg, respData)
	if err != nil {
		//n.logger.Warning("invalid incoming handshake resp bin data", err)
		conn.Close()
		return nil, fmt.Errorf("%s err: %v", errMsg, err)
	}

	err = p2p.ProcessHandshakeResponse(remotePublicKey, session, respData)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("%s err: %v", errMsg, err)
	}

	// TODO: Remove isAuthenticated - we announce state only when its authenticated
	// Session is now authenticated
	session.SetAuthenticated(true)

	conn.session = session
	conn.beginEventProcessing()
	return conn, nil
}


// Dial a remote server with provided time out
// address:: ip:port
// Returns established connection that local clients can send messages to or error if failed
// to establish a connection
func (n *netImpl) Dial(address string, remotePublicKey crypto.PublicKey, networkId int32, timeOut time.Duration, keepAlive time.Duration, secured bool) (*Connection, error) {
	var conn *Connection
	var err error
	if secured {
		conn, err = n.createSecuredConnection(address, remotePublicKey, networkId, timeOut, keepAlive)
	} else {
		conn, err = n.createConnection(address, timeOut, keepAlive)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to Dail. err: %v", err)
	}
	conn.beginEventProcessing()
	// update on new connection
	n.newConnections <- *conn

	return conn, nil
}

func (n *netImpl) Shutdown() {
	n.isShuttingDown = true
	n.tcpListener.Close()
}

// Start network server
func (n *netImpl) listen() error {
	n.logger.Info("Starting to listen...")
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
		n.logger.Debug("Waiting for incoming connections...")
		netConn, err := n.tcpListener.Accept()
		if err != nil {

			if !n.isShuttingDown {
				log.Error("Failed to accept connection request", err)
			}
			return
		}

		n.logger.Debug("Got new connection... Remote Address: %s", netConn.RemoteAddr())
		c := newConnection(netConn, n, Remote)
		n.newConnections <- c
	}
}
