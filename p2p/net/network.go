package net

import (
	"errors"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/delimited"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"gopkg.in/op/go-logging.v1"
	"net"
	"sync"
	"time"
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
	NetworkId() int
}

type remoteNode interface {
	PublicKey() crypto.PublicKey
}

type connChan struct {
	Chan chan *Connection
}

func NewConnChan() *connChan {
	return &connChan{make(chan *Connection, 20)}
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
	Dial(address string, remotePublicKey crypto.PublicKey, networkId int32) (*Connection, error) // Connect to a remote node. Can send when no error.
	Register(newConn *connChan)
	//GetNewConnections() chan *Connection
	GetClosingConnections() chan *Connection
	GetConnectionErrors() chan ConnectionError
	GetIncomingMessage() chan IncomingMessage
	GetPreSessionIncomingMessages() chan IncomingMessage
	GetMessageSendErrors() chan MessageSendError
	GetMessageSentCallback() chan MessageSentEvent
	GetLogger() *logging.Logger
	GetNetworkId() int32
	HandlePreSessionIncomingMessage(msg IncomingMessage) error
	GetLocalNode() localNode
	Shutdown()
}

// impl internal type
type netImpl struct {
	networkId int32
	localNode localNode
	logger    *logging.Logger

	tcpListener      net.Listener
	tcpListenAddress string // Address to open connection: localhost:9999

	//newConnections     chan *Connection
	closingConnections         chan *Connection
	connectionErrors           chan ConnectionError
	incomingMessages           chan IncomingMessage
	preSessionIncomingMessages chan IncomingMessage
	messageSendErrors          chan MessageSendError
	messageSentEvents          chan MessageSentEvent

	isShuttingDown bool

	regNewConn []chan *Connection
	regMutex   sync.RWMutex

	// remote connections that are pending session request
	pendingConn []*Connection

	config nodeconfig.Config
}

// NewNet creates a new network.
// It attempts to tcp listen on address. e.g. localhost:1234 .
func NewNet(tcpListenAddress string, conf nodeconfig.Config, logger *logging.Logger, localEntity localNode) (Net, error) {

	n := &netImpl{
		networkId:        int32(conf.NetworkID),
		localNode:        localEntity,
		logger:           logger,
		tcpListenAddress: tcpListenAddress,
		//newConnections:     make(chan *Connection, 20),
		closingConnections:         make(chan *Connection, 20),
		connectionErrors:           make(chan ConnectionError, 20),
		incomingMessages:           make(chan IncomingMessage, 20),
		preSessionIncomingMessages: make(chan IncomingMessage, 20),
		messageSendErrors:          make(chan MessageSendError, 20),
		messageSentEvents:          make(chan MessageSentEvent, 20),
		config:                     conf,
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
//func (n *netImpl) GetNewConnections() chan *Connection {
//	return n.newConnections
//}

func (n *netImpl) GetClosingConnections() chan *Connection {
	return n.closingConnections
}

func (n *netImpl) GetConnectionErrors() chan ConnectionError {
	return n.connectionErrors
}

func (n *netImpl) GetIncomingMessage() chan IncomingMessage {
	return n.incomingMessages
}

func (n *netImpl) GetPreSessionIncomingMessages() chan IncomingMessage {
	return n.preSessionIncomingMessages
}

func (n *netImpl) GetMessageSendErrors() chan MessageSendError {
	return n.messageSendErrors
}

func (n *netImpl) GetLogger() *logging.Logger {
	return n.logger
}

func (n *netImpl) GetNetworkId() int32 {
	return n.networkId
}

func (n *netImpl) GetLocalNode() localNode {
	return n.localNode
}

func (n *netImpl) createConnection(address string, remotePub crypto.PublicKey, timeOut time.Duration, keepAlive time.Duration) (*Connection, error) {
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
	c := newConnection(netConn, n, Local, remotePub)

	return c, nil
}

func (n *netImpl) createSecuredConnection(address string, remotePublicKey crypto.PublicKey, networkId int32, timeOut time.Duration, keepAlive time.Duration) (*Connection, error) {
	errMsg := "failed to establish secured connection."
	conn, err := n.createConnection(address, remotePublicKey, timeOut, keepAlive)
	if err != nil {
		return nil, err
	}
	data, session, err := GenerateHandshakeRequestData(n.localNode.PublicKey(), n.localNode.PrivateKey(), remotePublicKey, networkId)
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
	// TODO: support callback errors
	n.logger.Info("DEBUG: sending HS req")
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

	err = ProcessHandshakeResponse(remotePublicKey, session, respData)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("%s err: %v", errMsg, err)
	}

	// TODO: Remove isAuthenticated - we announce state only when its authenticated
	// Session is now authenticated
	//session.SetAuthenticated(true)

	conn.session = session
	return conn, nil
}

// Dial a remote server with provided time out
// address:: ip:port
// Returns established connection that local clients can send messages to or error if failed
// to establish a connection, currently only secured connections are supported
func (n *netImpl) Dial(address string, remotePublicKey crypto.PublicKey, networkId int32) (*Connection, error) {
	conn, err := n.createSecuredConnection(address, remotePublicKey, networkId, n.config.DialTimeout, n.config.ConnKeepAlive)
	if err != nil {
		return nil, fmt.Errorf("failed to Dail. err: %v", err)
	}

	go conn.beginEventProcessing()
	// update on new connection
	n.publishNewConnection(conn)

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
				//TODO only print to log and return? The node will continue running without the listener, doesn't sound healthy
			}
			return
		}

		n.logger.Debug("Got new connection... Remote Address: %s", netConn.RemoteAddr())
		c := newConnection(netConn, n, Remote, nil)

		go c.beginEventProcessing()
		// network won't publish the connection before it the remote node had established a session
	}
}

func (n *netImpl) Register(newConn *connChan) {
	n.regMutex.Lock()
	n.regNewConn = append(n.regNewConn, newConn.Chan)
	n.regMutex.Unlock()
}

func (n *netImpl) publishNewConnection(conn *Connection) {
	n.regMutex.RLock()
	for _, c := range n.regNewConn {
		c <- conn
	}
	n.regMutex.RUnlock()
}

func (n *netImpl) HandlePreSessionIncomingMessage(msg IncomingMessage) error {
	//TODO replace the next few lines with a way to validate that the message is a handshake request based on the message metadata
	errMsg := "failed to handle handshake request"
	data := &pb.HandshakeData{}
	err := proto.Unmarshal(msg.Message, data)
	if err != nil {
		return fmt.Errorf("%s. err: %v", errMsg, err)
	}

	//err = authenticateSenderNode(data)
	//if err != nil {
	//	return fmt.Errorf("%s. err: %v", errMsg, err)
	//}

	// new remote connection doesn't hold the remote public key until it gets the handshake request
	if msg.Connection.remotePub == nil {
		msg.Connection.remotePub, err = crypto.NewPublicKey(data.GetNodePubKey())
		n.GetLogger().Info("DEBUG: handling HS req from %v", msg.Connection.remotePub)
		if err != nil {
			return fmt.Errorf("%s. err: %v", errMsg, err)
		}
	}
	n.logger.Info("DEBUG: processing HS request")
	respData, session, err := ProcessHandshakeRequest(msg.Connection.net.GetNetworkId(), n.localNode.PublicKey(), n.localNode.PrivateKey(), msg.Connection.remotePub, data)

	// we have a new session started by a remote node
	//handshakeData := NewHandshakeData(h.swarm.GetLocalNode(), msg.Sender(), session, err)

	//if err != nil {
	//	// failed to process request
	//	handshakeData.SetError(err)
	//	h.stateChanged(handshakeData)
	//	return
	//}

	payload, err := proto.Marshal(respData)
	if err != nil {
		//handshakeData.SetError(err)
		//h.stateChanged(handshakeData)
		return fmt.Errorf("%s. err: %v", errMsg, err)
	}

	// TODO: support callback errors
	msg.Connection.Send(payload, session.ID())
	// send response back to sender
	//h.swarm.sendHandshakeMessage(SendMessageReq{
	//	ReqID:    session.ID(),
	//	PeerID:   msg.Sender().String(),
	//	Payload:  payload,
	//	Callback: nil,
	//})

	// we have an active session initiated by a remote node
	//h.stateChanged(handshakeData)

	//h.swarm.GetLocalNode().Debug("Remotely initiated session established. Session id: %s :-)", session.String())
	msg.Connection.session = session
	// update on new connection
	n.publishNewConnection(msg.Connection)
	return nil
}
