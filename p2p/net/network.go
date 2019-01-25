package net

import (
	"errors"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/delimited"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/version"
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

	closingConnections []chan Connection
	clsMutex           sync.RWMutex

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
		closingConnections:    make([]chan Connection, 0),
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

// SubscribeClosingConnections registers a channel where closing connections events are reported
func (n *Net) SubscribeClosingConnections() chan Connection {
	n.clsMutex.Lock()
	ch := make(chan Connection, 20) // todo: set a var for the buf size
	n.closingConnections = append(n.closingConnections, ch)
	n.clsMutex.Unlock()
	return ch
}

func (n *Net) publishClosingConnection(connection Connection) {
	n.clsMutex.RLock()
	for _, c := range n.closingConnections {
		c <- connection
	}
	n.clsMutex.RUnlock()
}

func dial(keepAlive, timeOut time.Duration, address string) (net.Conn, error) {
	// connect via dialer so we can set tcp network params
	dialer := &net.Dialer{}
	dialer.KeepAlive = keepAlive // drop connections after a period of inactivity
	dialer.Timeout = timeOut     // max time bef

	netConn, err := dialer.Dial("tcp", address)
	return netConn, err
}

func (n *Net) createConnection(address string, remotePub p2pcrypto.PublicKey, session NetworkSession,
	timeOut, keepAlive time.Duration) (ManagedConnection, error) {

	if n.isShuttingDown {
		return nil, fmt.Errorf("can't dial because the connection is shutting down")
	}

	n.logger.Debug("Dialing %v @ %v...", remotePub.String(), address)
	netConn, err := dial(keepAlive, timeOut, address)
	if err != nil {
		return nil, err
	}

	n.logger.Debug("Connected to %s...", address)
	formatter := delimited.NewChan(10)
	return newConnection(netConn, n, formatter, remotePub, session, n.logger), nil
}

func (n *Net) createSecuredConnection(address string, remotePubkey p2pcrypto.PublicKey, timeOut time.Duration,
	keepAlive time.Duration) (ManagedConnection, error) {

	session := createSession(n.localNode.PrivateKey(), remotePubkey)
	conn, err := n.createConnection(address, remotePubkey, session, timeOut, keepAlive)
	if err != nil {
		return nil, err
	}

	handshakeMessage, err := generateHandshakeMessage(session, n.networkID, n.tcpListenAddress.Port, n.localNode.PublicKey())
	if err != nil {
		conn.Close()
		return nil, err
	}
	err = conn.Send(handshakeMessage)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func createSession(privkey p2pcrypto.PrivateKey, remotePubkey p2pcrypto.PublicKey) NetworkSession {
	sharedSecret := p2pcrypto.GenerateSharedSecret(privkey, remotePubkey)
	session := NewNetworkSession(sharedSecret, remotePubkey)
	return session
}

// Dial a remote server with provided time out
// address:: ip:port
// Returns established connection that local clients can send messages to or error if failed
// to establish a connection, currently only secured connections are supported
func (n *Net) Dial(address string, remotePubkey p2pcrypto.PublicKey) (Connection, error) {
	conn, err := n.createSecuredConnection(address, remotePubkey, n.config.DialTimeout, n.config.ConnKeepAlive)
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
		c := newConnection(netConn, n, formatter, nil, nil, n.logger)

		go c.beginEventProcessing()
		// network won't publish the connection before it the remote node had established a session
	}
}

// SubscribeOnNewRemoteConnections returns new channel where events of new remote connections are reported
func (n *Net) SubscribeOnNewRemoteConnections() chan NewConnectionEvent {
	n.regMutex.Lock()
	ch := make(chan NewConnectionEvent, 30) // todo : the size should be determined after #269
	n.regNewRemoteConn = append(n.regNewRemoteConn, ch)
	n.regMutex.Unlock()
	return ch
}

func (n *Net) publishNewRemoteConnectionEvent(conn Connection, node node.Node) {
	n.regMutex.RLock()
	for _, c := range n.regNewRemoteConn {
		select {
		case c <- NewConnectionEvent{conn, node}:
			continue
		default:
			// so we won't block on not listening chans
		}
	}
	n.regMutex.RUnlock()
}

// HandlePreSessionIncomingMessage establishes session with the remote peer and update the Connection with the new session
func (n *Net) HandlePreSessionIncomingMessage(c Connection, message []byte) error {
	message, remotePubkey, err := p2pcrypto.ExtractPubkey(message)
	if err != nil {
		return err
	}
	c.SetRemotePublicKey(remotePubkey)
	session := createSession(n.localNode.PrivateKey(), remotePubkey)
	c.SetSession(session)

	// open message
	protoMessage, err := session.OpenMessage(message)
	if err != nil {
		return err
	}

	handshakeData := &pb.HandshakeData{}
	err = proto.Unmarshal(protoMessage, handshakeData)
	if err != nil {
		return err
	}

	err = n.verifyNetworkIDAndClientVersion(handshakeData)
	if err != nil {
		return err
	}
	remoteListeningPort := uint16(handshakeData.Port)
	remoteListeningAddress, err := replacePort(c.RemoteAddr().String(), remoteListeningPort)
	if err != nil {
		return err
	}
	anode := node.New(c.RemotePublicKey(), remoteListeningAddress)

	n.publishNewRemoteConnectionEvent(c, anode)
	// TODO: @noam process message?
	// Specifically -- check the network id and client version
	return nil
}

func (n *Net) verifyNetworkIDAndClientVersion(handshakeData *pb.HandshakeData) error {
	// check that received clientversion is valid client string
	reqVersion := strings.Split(handshakeData.ClientVersion, "/")
	if len(reqVersion) != 2 {
		return errors.New("invalid client version")
	}

	// compare that version to the min client version in config
	ok, err := version.CheckNodeVersion(reqVersion[1], config.MinClientVersion)
	if err == nil && !ok {
		return errors.New("unsupported client version")
	}
	if err != nil {
		return fmt.Errorf("invalid client version, err: %v", err)
	}

	// make sure we're on the same network
	if handshakeData.NetworkID != int32(n.networkID) {
		return fmt.Errorf("request net id (%d) is different than local net id (%d)", handshakeData.NetworkID, n.networkID)
		//TODO : drop and blacklist this sender
	}
	return nil
}

func generateHandshakeMessage(session NetworkSession, networkID int8, localIncomingPort int, localPubkey p2pcrypto.PublicKey) ([]byte, error) {
	handshakeData := &pb.HandshakeData{
		Timestamp:            time.Now().Unix(),
		ClientVersion:        config.ClientVersion,
		NetworkID:            int32(networkID),
		Port:                 uint32(localIncomingPort),
	}
	handshakeMessage, err := proto.Marshal(handshakeData)
	if err != nil {
		return nil, err
	}
	sealedMessage := session.SealMessage(handshakeMessage)
	return p2pcrypto.PrependPubkey(sealedMessage, localPubkey), nil
}

func replacePort(addr string, newPort uint16) (string, error) {
	addrWithoutPort, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid address format, (%v) err: %v", addr, err)
	}
	return net.JoinHostPort(addrWithoutPort, strconv.Itoa(int(newPort))), nil
}
