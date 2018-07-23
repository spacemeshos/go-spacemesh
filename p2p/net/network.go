package net

import (
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/net/wire"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"gopkg.in/op/go-logging.v1"
	"net"
	"sync"
	"time"
)

// Net is a connection manager able to dial remote endpoints
// Net clients should register all callbacks
// Connections may be initiated by Dial() or by remote clients connecting to the listen address
// ConnManager includes a TCP server, and a TCP client
// It provides full duplex messaging functionality over the same tcp/ip connection
// Network should not know about higher-level networking types such as remoteNode, swarm and networkSession
// Network main client is the swarm
// Net has no channel events processing loops - clients are responsible for polling these channels and popping events from them
type Net interface {
	Dial(address string, remotePublicKey crypto.PublicKey, networkId int8) (Connectioner, error) // Connect to a remote node. Can send when no error.
	SubscribeOnNewRemoteConnections() chan Connectioner
	GetLogger() *logging.Logger
	GetNetworkId() int8
	HandlePreSessionIncomingMessage(c Connectioner, msg wire.InMessage) error
	GetLocalNode() *node.LocalNode
	IncomingMessages() chan IncomingMessageEvent
	ClosingConnections() chan Connectioner
	Shutdown()
}

type IncomingMessageEvent struct {
	Conn    Connectioner
	Message []byte
}

// impl internal type
type netImpl struct {
	networkId int8
	localNode *node.LocalNode
	logger    *logging.Logger

	tcpListener      net.Listener
	tcpListenAddress string // Address to open connection: localhost:9999\

	isShuttingDown bool

	regNewRemoteConn []chan Connectioner
	regMutex         sync.RWMutex

	incomingMessages   chan IncomingMessageEvent
	closingConnections chan Connectioner

	config config.Config
}

// NewNet creates a new network.
// It attempts to tcp listen on address. e.g. localhost:1234 .
func NewNet(conf config.Config, localEntity *node.LocalNode) (Net, error) {

	n := &netImpl{
		networkId:        conf.NetworkID,
		localNode:        localEntity,
		logger:           localEntity.Logger,
		tcpListenAddress: localEntity.Address(),
		regNewRemoteConn:			make([]chan Connectioner, 0),
		incomingMessages:   make(chan IncomingMessageEvent),
		closingConnections: make(chan Connectioner, 20),
		config:           conf,
	}

	err := n.listen()

	if err != nil {
		return nil, err
	}

	n.logger.Debug("created network with tcp address: %s", n.tcpListenAddress)

	return n, nil
}

func (n *netImpl) GetLogger() *logging.Logger {
	return n.logger
}

func (n *netImpl) GetNetworkId() int8 {
	return n.networkId
}

func (n *netImpl) GetLocalNode() *node.LocalNode {
	return n.localNode
}

func (n *netImpl) IncomingMessages() chan IncomingMessageEvent {
	return n.incomingMessages
}

func (n *netImpl) ClosingConnections() chan Connectioner {
	return n.closingConnections
}

func (n *netImpl) createConnection(address string, remotePub crypto.PublicKey, timeOut time.Duration, keepAlive time.Duration) (Connectioner, error) {
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
	c := newConnection(netConn, n, Local, remotePub, n.logger)

	return c, nil
}

func (n *netImpl) createSecuredConnection(address string, remotePublicKey crypto.PublicKey, networkId int8, timeOut time.Duration, keepAlive time.Duration) (Connectioner, error) {
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
	n.logger.Debug("Creating session handshake request session id: %s", session)
	payload, err := proto.Marshal(data)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("%s err: %v", errMsg, err)
	}

	err = conn.Send(payload)
	if err != nil {
		return nil, err
	}

	var msg wire.InMessage
	timer := time.NewTimer(n.config.ResponseTimeout)
	select {
	case msg, ok := <-conn.IncomingChannel():
		if !ok {
			return nil, fmt.Errorf("%s err: incoming channel got closed", errMsg)
		}
		if msg.Error() != nil {
			return nil, fmt.Errorf("%s err: %v", errMsg, msg.Error())
		}
	case <-timer.C:
		n.logger.Info("waiting for HS response timed-out. remoteKey=%v", remotePublicKey)
		return nil, fmt.Errorf("%s err: HS response timed-out", errMsg)
	}

	respData := &pb.HandshakeData{}
	err = proto.Unmarshal(msg.Message(), respData)
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

	conn.SetSession(session)
	return conn, nil
}

// Dial a remote server with provided time out
// address:: ip:port
// Returns established connection that local clients can send messages to or error if failed
// to establish a connection, currently only secured connections are supported
func (n *netImpl) Dial(address string, remotePublicKey crypto.PublicKey, networkId int8) (Connectioner, error) {
	conn, err := n.createSecuredConnection(address, remotePublicKey, networkId, n.config.DialTimeout, n.config.ConnKeepAlive)
	if err != nil {
		return nil, fmt.Errorf("failed to Dail. err: %v", err)
	}
	go conn.beginEventProcessing()
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
		c := newConnection(netConn, n, Remote, nil, n.logger)

		go c.beginEventProcessing()
		// network won't publish the connection before it the remote node had established a session
	}
}

func (n *netImpl) SubscribeOnNewRemoteConnections() chan Connectioner {
	n.regMutex.Lock()
	ch := make(chan Connectioner, 20)
	n.regNewRemoteConn = append(n.regNewRemoteConn, ch)
	n.regMutex.Unlock()
	return ch
}

func (n *netImpl) publishNewRemoteConnection(conn Connectioner) {
	n.regMutex.RLock()
	for _, c := range n.regNewRemoteConn {
		c <- conn
	}
	n.regMutex.RUnlock()
}

func (n *netImpl) HandlePreSessionIncomingMessage(c Connectioner, message wire.InMessage) error {
	//TODO replace the next few lines with a way to validate that the message is a handshake request based on the message metadata
	errMsg := "failed to handle handshake request"
	data := &pb.HandshakeData{}

	err := proto.Unmarshal(message.Message(), data)
	if err != nil {
		return fmt.Errorf("%s. err: %v", errMsg, err)
	}

	// new remote connection doesn't hold the remote public key until it gets the handshake request
	if c.RemotePublicKey() == nil {
		rPub, err := crypto.NewPublicKey(data.GetNodePubKey())
		n.GetLogger().Info("DEBUG: handling HS req from %v", rPub)
		if err != nil {
			return fmt.Errorf("%s. err: %v", errMsg, err)
		}
		c.SetRemotePublicKey(rPub)

	}
	respData, session, err := ProcessHandshakeRequest(n.GetNetworkId(), n.localNode.PublicKey(), n.localNode.PrivateKey(), c.RemotePublicKey(), data)
	payload, err := proto.Marshal(respData)
	if err != nil {
		return fmt.Errorf("%s. err: %v", errMsg, err)
	}

	err = c.Send(payload)
	if err != nil {
		return err
	}

	c.SetSession(session)
	// update on new connection
	n.publishNewRemoteConnection(c)
	return nil
}
