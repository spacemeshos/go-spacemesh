package net

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/version"
	"net"
	"strconv"
	"sync"
	//"syscall"
	"time"
)

// DefaultQueueCount is the default number of messages queue we hold. messages queues are used to serialize message receiving
const DefaultQueueCount uint = 6

// DefaultMessageQueueSize is the buffer size of each queue mentioned above. (queues are buffered channels)
const DefaultMessageQueueSize uint = 5120

// IncomingMessageEvent is the event reported on new incoming message, it contains the message and the Connection carrying the message
type IncomingMessageEvent struct {
	Conn    Connection
	Message []byte
}

// ManagedConnection in an interface extending Connection with some internal methods that are required for Net to manage Connections
type ManagedConnection interface {
	Connection
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
	localNode node.LocalNode
	logger    log.Log

	listener      net.Listener
	listenAddress *net.TCPAddr // Address to open connection: localhost:9999\

	isShuttingDown bool

	regMutex         sync.RWMutex
	regNewRemoteConn []func(NewConnectionEvent)

	clsMutex           sync.RWMutex
	closingConnections []func(ConnectionWithErr)

	queuesCount           uint
	incomingMessagesQueue []chan IncomingMessageEvent

	config config.Config
}

// NewConnectionEvent is a struct holding a new created connection and a node info.
type NewConnectionEvent struct {
	Conn Connection
	Node *node.NodeInfo
}

// TODO Create a config for Net and only pass it in.

// NewNet creates a new network.
// It attempts to tcp listen on address. e.g. localhost:1234 .
func NewNet(conf config.Config, localEntity node.LocalNode, addr *net.TCPAddr, logger log.Log) (*Net, error) {

	qcount := DefaultQueueCount      // todo : get from cfg
	qsize := DefaultMessageQueueSize // todo : get from cfg

	n := &Net{
		networkID:             conf.NetworkID,
		localNode:             localEntity,
		logger:                logger,
		listenAddress:         addr,
		regNewRemoteConn:      make([]func(NewConnectionEvent), 0, 3),
		closingConnections:    make([]func(cwe ConnectionWithErr), 0, 3),
		queuesCount:           qcount,
		incomingMessagesQueue: make([]chan IncomingMessageEvent, qcount, qcount),
		config:                conf,
	}

	for imq := range n.incomingMessagesQueue {
		n.incomingMessagesQueue[imq] = make(chan IncomingMessageEvent, qsize)
	}

	return n, nil
}

func (n *Net) Start() error { // todo: maybe add context
	err := n.listen(n.newTcpListener)
	if err != nil {
		return err
	}
	n.logger.Info("Started TCP server listener for connections on tcp:%v", n.listener.Addr().String())
	return nil
}

// LocalAddr returns the local listening address. panics before calling Start or if Start errored
func (n *Net) LocalAddr() net.Addr {
	return n.listener.Addr()
}

// Logger returns a reference to logger
func (n *Net) Logger() log.Log {
	return n.logger
}

// NetworkID retuers Net's network ID
func (n *Net) NetworkID() int8 {
	return n.networkID
}

// LocalNode return's the local node descriptor
func (n *Net) LocalNode() node.LocalNode {
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

// SubscribeClosingConnections registers a callback for a new connection event. all registered callbacks are called before moving.
func (n *Net) SubscribeClosingConnections(f func(connection ConnectionWithErr)) {
	n.clsMutex.Lock()
	n.closingConnections = append(n.closingConnections, f)
	n.clsMutex.Unlock()
}

func (n *Net) publishClosingConnection(connection ConnectionWithErr) {
	n.clsMutex.RLock()
	for _, f := range n.closingConnections {
		f(connection)
	}
	n.clsMutex.RUnlock()
}

func dial(keepAlive, timeOut time.Duration, address net.Addr) (net.Conn, error) {
	// connect via dialer so we can set tcp network params
	dialer := &net.Dialer{}
	dialer.Timeout = timeOut // max time bef

	netConn, err := dialer.Dial("tcp", address.String())
	if err == nil {
		tcpconn := netConn.(*net.TCPConn)
		tcpSocketConfig(tcpconn)
		return tcpconn, nil
		//netConn = tcpconn
	}
	return netConn, err
}

func tcpSocketConfig(tcpconn *net.TCPConn) {
	// TODO: Error handling, what if only certain flags are supported
	// TODO: Parameters, try to find right buffers based on something or os/net-interface input?
	tcpconn.SetReadBuffer(5 * 1024 * 1024)
	tcpconn.SetWriteBuffer(5 * 1024 * 1024)

	tcpconn.SetKeepAlive(true)
	tcpconn.SetKeepAlivePeriod(time.Second * 10)
}

func (n *Net) createConnection(address net.Addr, remotePub p2pcrypto.PublicKey, session NetworkSession,
	timeOut, keepAlive time.Duration) (ManagedConnection, error) {

	if n.isShuttingDown {
		return nil, fmt.Errorf("can't dial because the connection is shutting down")
	}

	n.logger.Debug("Dialing %v @ %v...", remotePub.String(), address.String())
	netConn, err := dial(keepAlive, timeOut, address)
	if err != nil {
		return nil, err
	}

	n.logger.Debug("Connected to %s...", address.String())
	return newConnection(netConn, n, remotePub, session, n.config.MsgSizeLimit, n.config.ResponseTimeout, n.logger), nil
}

func (n *Net) createSecuredConnection(address net.Addr, remotePubkey p2pcrypto.PublicKey, timeOut time.Duration,
	keepAlive time.Duration) (ManagedConnection, error) {

	session := createSession(n.localNode.PrivateKey(), remotePubkey)
	conn, err := n.createConnection(address, remotePubkey, session, timeOut, keepAlive)
	if err != nil {
		return nil, err
	}

	handshakeMessage, err := generateHandshakeMessage(session, n.networkID, n.listenAddress.Port, n.localNode.PublicKey())
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
// address:: net.Addr
// Returns established connection that local clients can send messages to or error if failed
// to establish a connection, currently only secured connections are supported
func (n *Net) Dial(address net.Addr, remotePubkey p2pcrypto.PublicKey) (Connection, error) {
	conn, err := n.createSecuredConnection(address, remotePubkey, n.config.DialTimeout, n.config.ConnKeepAlive)
	if err != nil {
		return nil, fmt.Errorf("failed to Dial. err: %v", err)
	}
	go conn.beginEventProcessing()
	return conn, nil
}

// Shutdown initiate a graceful closing of the TCP listener and all other internal routines
func (n *Net) Shutdown() {
	n.isShuttingDown = true
	if n.listener != nil {
		err := n.listener.Close()
		if err != nil {
			n.logger.Error("Error closing listener err=%v", err)
		}
	}
}

func (n *Net) newTcpListener() (net.Listener, error) {
	tcpListener, err := net.Listen("tcp", n.listenAddress.String())
	if err != nil {
		return nil, err
	}
	return tcpListener, nil
}

// Start network server
func (n *Net) listen(lis func() (listener net.Listener, err error)) error {
	listener, err := lis()
	n.listener = listener
	if err != nil {
		return err
	}
	go n.accept(listener)
	return nil
}

func (n *Net) accept(listen net.Listener) {
	n.logger.Debug("Waiting for incoming connections...")
	pending := make(chan struct{}, n.config.MaxPendingConnections)

	for i := 0; i < n.config.MaxPendingConnections; i++ {
		pending <- struct{}{}
	}

	for {
		<-pending
		netConn, err := listen.Accept()
		if err != nil {
			if n.isShuttingDown {
				return
			}
			if !Temporary(err) {
				n.logger.Error("Listener errored while accepting connections: err: %v", err)
				return
			}

			n.logger.Warning("Failed to accept connection request err:%v", err)
			pending <- struct{}{}
			continue
		}

		n.logger.Debug("Got new connection... Remote Address: %s", netConn.RemoteAddr())
		conn := netConn.(*net.TCPConn)
		tcpSocketConfig(conn) // TODO maybe only set this after session handshake to prevent denial of service with big messages
		c := newConnection(netConn, n, nil, nil, n.config.MsgSizeLimit, n.config.ResponseTimeout, n.logger)
		go func(con Connection) {
			defer func() { pending <- struct{}{} }()
			err := c.setupIncoming(n.config.SessionTimeout)
			if err != nil {
				n.logger.Event().Warning("conn_incoming_failed", log.String("remote", c.remoteAddr.String()), log.Err(err))
				return
			}
			go c.beginEventProcessing()
		}(c)

		// network won't publish the connection before it the remote node had established a session
	}
}

// SubscribeOnNewRemoteConnections registers a callback for a new connection event. all registered callbacks are called before moving.
func (n *Net) SubscribeOnNewRemoteConnections(f func(event NewConnectionEvent)) {
	n.regMutex.Lock()
	n.regNewRemoteConn = append(n.regNewRemoteConn, f)
	n.regMutex.Unlock()
}

func (n *Net) publishNewRemoteConnectionEvent(conn Connection, node *node.NodeInfo) {
	n.regMutex.RLock()
	for _, f := range n.regNewRemoteConn {
		f(NewConnectionEvent{conn, node})
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

	handshakeData := &HandshakeData{}
	err = types.BytesToInterface(protoMessage, handshakeData)
	if err != nil {
		return err
	}

	err = verifyNetworkIDAndClientVersion(n.networkID, handshakeData)
	if err != nil {
		return err
	}
	// TODO: pass TO - IP:port and FROM - IP:port in handshake message.
	remoteListeningPort := handshakeData.Port
	remoteListeningAddress, err := replacePort(c.RemoteAddr().String(), remoteListeningPort)
	if err != nil {
		return err
	}
	anode := node.NewNode(c.RemotePublicKey(), net.ParseIP(remoteListeningAddress), remoteListeningPort, remoteListeningPort)

	n.publishNewRemoteConnectionEvent(c, anode)
	return nil
}

func verifyNetworkIDAndClientVersion(networkID int8, handshakeData *HandshakeData) error {
	// compare that version to the min client version in config
	ok, err := version.CheckNodeVersion(handshakeData.ClientVersion, config.MinClientVersion)
	if err == nil && !ok {
		return errors.New("unsupported client version")
	}
	if err != nil {
		return fmt.Errorf("invalid client version, err: %v", err)
	}

	// make sure we're on the same network
	if handshakeData.NetworkID != int32(networkID) {
		return fmt.Errorf("request net id (%d) is different than local net id (%d)", handshakeData.NetworkID, networkID)
		//TODO : drop and blacklist this sender
	}
	return nil
}

func generateHandshakeMessage(session NetworkSession, networkID int8, localIncomingPort int, localPubkey p2pcrypto.PublicKey) ([]byte, error) {
	handshakeData := &HandshakeData{
		ClientVersion: config.ClientVersion,
		NetworkID:     int32(networkID),
		Port:          uint16(localIncomingPort),
	}
	handshakeMessage, err := types.InterfaceToBytes(handshakeData)
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
