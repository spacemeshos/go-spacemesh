package net

import (
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/delimited"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"gopkg.in/op/go-logging.v1"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultQueueCount is the default number of messages queue we hold. messages queues are used to serialize message receiving
const DefaultQueueCount uint = 6

// DefaultMessageQueueSize is the buffer size of each queue mentioned above. (queues are buffered channels)
const DefaultMessageQueueSize uint = 256

// IncomingMessageEvent is the event reported on new incoming message, it contains the message and the Connection carrying the message
type IncomingMessageEvent struct {
	Conn    Connection
	Message []byte
}

// ManagedConnection in an interface extending Connection with some internal methods that are required for Net to manage Connections
type ManagedConnection interface {
	Connection
	incomingChannel() chan []byte
	beginEventProcessing()
}

// Net is a connection factory able to dial remote endpoints
// Net clients should register all callbacks
// Connections may be initiated by Dial() or by remote clients connecting to the listen address
// ConnManager includes a TCP server, and a TCP client
// It provides full duplex messaging functionality over the same tcp/ip connection
// Network should not know about higher-level networking types such as remoteNode, swarm and networkSession
// Network main client is the swarm
// Net has no channel events processing loops - clients are responsible for polling these channels and popping events from them
type Net struct {
	networkID int8
	localNode *node.LocalNode
	logger    *logging.Logger

	tcpListener      net.Listener
	tcpListenAddress *net.TCPAddr // Address to open connection: localhost:9999\

	isShuttingDown bool

	regNewRemoteConn []chan NewConnectionEvent
	regMutex         sync.RWMutex

	queuesCount           uint
	incomingMessagesQueue []chan IncomingMessageEvent

	closingConnections chan Connection

	config config.Config
}

// NewConnectionEvent is a struct holding a new created connection and a node info.
type NewConnectionEvent struct {
	Conn Connection
	Node node.Node
}

// NewNet creates a new network.
// It attempts to tcp listen on address. e.g. localhost:1234 .
func NewNet(conf config.Config, localEntity *node.LocalNode) (*Net, error) {

	qcount := DefaultQueueCount      // todo : get from cfg
	qsize := DefaultMessageQueueSize // todo : get from cfg

	tcpAddress, err := net.ResolveTCPAddr("tcp", localEntity.Address())
	if err != nil {
		fmt.Errorf("can't resolve local address: %v, err:%v", localEntity.Address(), err)
	}

	n := &Net{
		networkID:             conf.NetworkID,
		localNode:             localEntity,
		logger:                localEntity.Logger,
		tcpListenAddress:      tcpAddress,
		regNewRemoteConn:      make([]chan NewConnectionEvent, 0),
		queuesCount:           qcount,
		incomingMessagesQueue: make([]chan IncomingMessageEvent, qcount, qcount),
		closingConnections:    make(chan Connection, 20),
		config:                conf,
	}

	for imq := range n.incomingMessagesQueue {
		n.incomingMessagesQueue[imq] = make(chan IncomingMessageEvent, qsize)
	}

	err = n.listen()

	if err != nil {
		return nil, err
	}

	n.logger.Debug("created network with tcp address: %s", n.tcpListenAddress)

	return n, nil
}

// Logger returns a reference to logger
func (n *Net) Logger() *logging.Logger {
	return n.logger
}

// NetworkID retuers Net's network ID
func (n *Net) NetworkID() int8 {
	return n.networkID
}

// LocalNode return's the local node descriptor
func (n *Net) LocalNode() *node.LocalNode {
	return n.localNode
}

// sumByteArray sums all bytes in an array as uint
func sumByteArray(b []byte) uint {
	var sumOfChars uint

	//take each byte in the string and add the values
	for i := 0; i < len(b); i++ {
		byteVal := b[i]
		sumOfChars += uint(byteVal)
	}
	return sumOfChars
}

// EnqueueMessage inserts a message into a queue, to decide on which queue to send the message to
// it sum the remote public key bytes as integer to segment to queueCount queues.
func (n *Net) EnqueueMessage(event IncomingMessageEvent) {
	sba := sumByteArray(event.Conn.RemotePublicKey().Bytes())
	n.incomingMessagesQueue[sba%n.queuesCount] <- event
}

// IncomingMessages returns a slice of channels which incoming messages are delivered on
// the receiver should iterate  on all the channels and read all messages. to sync messages order but enable parallel messages handling.
func (n *Net) IncomingMessages() []chan IncomingMessageEvent {
	return n.incomingMessagesQueue
}

// ClosingConnections returns Net's channel where closing connections events are reported
func (n *Net) ClosingConnections() chan Connection {
	return n.closingConnections
}

func (n *Net) createConnection(address string, remotePub crypto.PublicKey, timeOut time.Duration, keepAlive time.Duration) (ManagedConnection, error) {
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
	formatter := delimited.NewChan(10)
	c := newConnection(netConn, n, formatter, remotePub, n.logger)

	return c, nil
}

func (n *Net) createSecuredConnection(address string, remotePublicKey crypto.PublicKey, timeOut time.Duration, keepAlive time.Duration) (ManagedConnection, error) {
	errMsg := "failed to establish secured connection."
	conn, err := n.createConnection(address, remotePublicKey, timeOut, keepAlive)
	if err != nil {
		return nil, err
	}
	data, session, err := GenerateHandshakeRequestData(n.localNode.PublicKey(), n.localNode.PrivateKey(), remotePublicKey, n.networkID, uint16(n.tcpListenAddress.Port))
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
		conn.Close()
		return nil, err
	}

	var msg []byte
	var ok bool
	timer := time.NewTimer(n.config.ResponseTimeout)
	select {
	case msg, ok = <-conn.incomingChannel():
		if !ok {
			conn.Close()
			return nil, fmt.Errorf("%s err: incoming channel got closed with %v", errMsg, conn.RemotePublicKey())
		}
	case <-timer.C:
		n.logger.Info("waiting for HS response timed-out. remoteKey=%v", remotePublicKey)
		conn.Close()
		return nil, fmt.Errorf("%s err: HS response timed-out", errMsg)
	}

	respData := &pb.HandshakeData{}
	err = proto.Unmarshal(msg, respData)
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
func (n *Net) Dial(address string, remotePublicKey crypto.PublicKey) (Connection, error) {
	conn, err := n.createSecuredConnection(address, remotePublicKey, n.config.DialTimeout, n.config.ConnKeepAlive)
	if err != nil {
		return nil, fmt.Errorf("failed to Dail. err: %v", err)
	}
	go conn.beginEventProcessing()
	return conn, nil
}

// Shutdown initiate a graceful closing of the TCP listener and all other internal routines
func (n *Net) Shutdown() {
	n.isShuttingDown = true
	n.tcpListener.Close()
}

// Start network server
func (n *Net) listen() error {
	n.logger.Info("Starting to listen on %v", n.tcpListenAddress)
	tcpListener, err := net.Listen("tcp", n.tcpListenAddress.String())
	if err != nil {
		return err
	}
	n.tcpListener = tcpListener
	go n.acceptTCP()
	return nil
}

func (n *Net) acceptTCP() {
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
		formatter := delimited.NewChan(10)
		c := newConnection(netConn, n, formatter, nil, n.logger)

		go c.beginEventProcessing()
		// network won't publish the connection before it the remote node had established a session
	}
}

// SubscribeOnNewRemoteConnections returns new channel where events of new remote connections are reported
func (n *Net) SubscribeOnNewRemoteConnections() chan NewConnectionEvent {
	n.regMutex.Lock()
	ch := make(chan NewConnectionEvent, 20)
	n.regNewRemoteConn = append(n.regNewRemoteConn, ch)
	n.regMutex.Unlock()
	return ch
}

func (n *Net) publishNewRemoteConnectionEvent(conn Connection, node node.Node) {
	n.regMutex.RLock()
	for _, c := range n.regNewRemoteConn {
		c <- NewConnectionEvent{conn, node}
	}
	n.regMutex.RUnlock()
}

// HandlePreSessionIncomingMessage establishes session with the remote peer and update the Connection with the new session
func (n *Net) HandlePreSessionIncomingMessage(c Connection, message []byte) error {
	//TODO replace the next few lines with a way to validate that the message is a handshake request based on the message metadata
	errMsg := "failed to handle handshake request"
	data := &pb.HandshakeData{}

	err := proto.Unmarshal(message, data)
	if err != nil {
		return fmt.Errorf("%s. err: %v", errMsg, err)
	}

	// new remote connection doesn't hold the remote public key until it gets the handshake request
	if c.RemotePublicKey() == nil {
		rPub, err := crypto.NewPublicKey(data.GetNodePubKey())
		n.Logger().Debug("DEBUG: handling HS req from %v", rPub)
		if err != nil {
			return fmt.Errorf("%s. err: %v", errMsg, err)
		}
		c.SetRemotePublicKey(rPub)
	}
	respData, session, err := ProcessHandshakeRequest(n.NetworkID(), n.localNode.PublicKey(), n.localNode.PrivateKey(), c.RemotePublicKey(), data)
	if err != nil {
		return fmt.Errorf("%s. err: %v", errMsg, err)
	}
	payload, err := proto.Marshal(respData)
	if err != nil {
		return fmt.Errorf("%s. hereto err: %v", errMsg, err)
	}

	err = c.Send(payload)
	if err != nil {
		return err
	}

	c.SetSession(session)

	// update on new connection
	addr := strings.Split(c.RemoteAddr().String(), ":")[0] // this should never be bad unless address is corrupted
	anode := node.New(c.RemotePublicKey(), net.JoinHostPort(addr, strconv.Itoa(int(data.Port))))
	n.publishNewRemoteConnectionEvent(c, anode)

	return nil
}
