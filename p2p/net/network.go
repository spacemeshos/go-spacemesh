// Package net manages the accepting network connections/messages and routing the data upward for the protocols to consume.
package net

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/version"
)

// DefaultQueueCount is the default number of messages queue we hold. messages queues are used to serialize message receiving
const DefaultQueueCount uint = 6

// DefaultMessageQueueSize is the buffer size of each queue mentioned above. (queues are buffered channels)
const DefaultMessageQueueSize uint = 5120

const (
	// ReadBufferSize const is the default value used to set the socket option SO_RCVBUF
	ReadBufferSize = 5 * 1024 * 1024
	// WriteBufferSize const is the default value used to set the socket option SO_SNDBUF
	WriteBufferSize = 5 * 1024 * 1024
	// TCPKeepAlive sets whether KeepAlive is active or not on the socket
	TCPKeepAlive = true
	// TCPKeepAlivePeriod sets the interval of KeepAlive
	TCPKeepAlivePeriod = 10 * time.Second
)

// IncomingMessageEvent is the event reported on new incoming message, it contains the message and the Connection carrying the message
type IncomingMessageEvent struct {
	Conn      Connection
	Message   []byte
	RequestID string
}

// ManagedConnection in an interface extending Connection with some internal methods that are required for Net to manage Connections
type ManagedConnection interface {
	Connection
	beginEventProcessing(ctx context.Context)
}

// Net is a connection factory able to dial remote endpoints
// Net clients should register all callbacks
// Connections may be initiated by Dial() or by remote clients connecting to the listen address
// It provides full duplex messaging functionality over the same tcp/ip connection
// Net has no channel events processing loops - clients are responsible for polling these channels and popping events from them
type Net struct {
	networkID uint32
	localNode node.LocalNode
	logger    log.Log

	listener      net.Listener
	listenAddress *net.TCPAddr // Address to open connection: localhost:9999

	shutdownCtx context.Context

	regMutex         sync.RWMutex
	regNewRemoteConn []func(NewConnectionEvent)

	clsMutex           sync.RWMutex
	closingConnections []func(context.Context, ConnectionWithErr)

	queuesCount           uint
	incomingMessagesQueue []chan IncomingMessageEvent

	config config.Config
}

// NewConnectionEvent is a struct holding a new created connection and a node info.
type NewConnectionEvent struct {
	Conn Connection
	Node *node.Info
}

// TODO Create a config for Net and only pass it in.

// NewNet creates a new network.
// It attempts to tcp listen on address. e.g. localhost:1234 .
func NewNet(ctx context.Context, conf config.Config, localEntity node.LocalNode, logger log.Log) (*Net, error) {
	qcount := DefaultQueueCount      // todo : get from cfg
	qsize := DefaultMessageQueueSize // todo : get from cfg

	n := &Net{
		networkID:             conf.NetworkID,
		localNode:             localEntity,
		logger:                logger,
		regNewRemoteConn:      make([]func(NewConnectionEvent), 0, 3),
		closingConnections:    make([]func(context.Context, ConnectionWithErr), 0, 3),
		queuesCount:           qcount,
		incomingMessagesQueue: make([]chan IncomingMessageEvent, qcount),
		config:                conf,
		shutdownCtx:           ctx,
	}

	for imq := range n.incomingMessagesQueue {
		n.incomingMessagesQueue[imq] = make(chan IncomingMessageEvent, qsize)
	}

	return n, nil
}

// Start begins accepting connections from the listener socket
func (n *Net) Start(ctx context.Context, listener net.Listener) { // todo: maybe add context
	n.listener = listener
	n.listenAddress = listener.Addr().(*net.TCPAddr)
	n.logger.WithContext(ctx).With().Info("started tcp server listening for connections",
		log.String("tcpaddr", listener.Addr().String()))

	// This channel enforces the limit on the number of incoming connections
	// Each message sent to the channel represents a "token" that allows one additional incoming connection
	pending := make(chan struct{}, n.config.MaxPendingConnections)
	for i := 0; i < n.config.MaxPendingConnections; i++ {
		pending <- struct{}{}
	}

	go n.accept(ctx, listener, pending)
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
func (n *Net) NetworkID() uint32 {
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
func (n *Net) EnqueueMessage(ctx context.Context, event IncomingMessageEvent) {
	sba := sumByteArray(event.Conn.RemotePublicKey().Bytes())
	n.incomingMessagesQueue[sba%n.queuesCount] <- event
}

// IncomingMessages returns a slice of channels which incoming messages are delivered on
// the receiver should iterate  on all the channels and read all messages. to sync messages order but enable parallel messages handling.
func (n *Net) IncomingMessages() []chan IncomingMessageEvent {
	return n.incomingMessagesQueue
}

// SubscribeClosingConnections registers a callback for a new connection event. all registered callbacks are called before moving.
func (n *Net) SubscribeClosingConnections(f func(context.Context, ConnectionWithErr)) {
	n.clsMutex.Lock()
	n.closingConnections = append(n.closingConnections, f)
	n.clsMutex.Unlock()
}

func (n *Net) publishClosingConnection(connection ConnectionWithErr) {
	n.clsMutex.RLock()
	for _, f := range n.closingConnections {
		f(context.TODO(), connection)
	}
	n.clsMutex.RUnlock()
}

func (n *Net) dial(ctx context.Context, address net.Addr) (net.Conn, error) {
	// connect via dialer so we can set tcp network params
	dialer := &net.Dialer{}
	netConn, err := dialer.DialContext(ctx, "tcp", address.String())
	if err == nil {
		tcpconn := netConn.(*net.TCPConn)
		n.tcpSocketConfig(tcpconn)
		return tcpconn, nil
	}
	return netConn, err
}

func (n *Net) tcpSocketConfig(tcpconn *net.TCPConn) {
	// TODO: Parameters, try to find right buffers based on something or os/net-interface input?
	if err := tcpconn.SetReadBuffer(ReadBufferSize); err != nil {
		n.logger.Warning("Error trying to set ReadBuffer on TCPConn %v", err)
	}
	if err := tcpconn.SetWriteBuffer(WriteBufferSize); err != nil {
		n.logger.Warning("Error trying to set WriteBuffer on TCPConn %v", err)
	}

	if err := tcpconn.SetKeepAlive(TCPKeepAlive); err != nil {
		n.logger.Warning("Error trying to set KeepAlive on TCPConn %v", err)
	}
	if err := tcpconn.SetKeepAlivePeriod(TCPKeepAlivePeriod); err != nil {
		n.logger.Warning("Error trying to set KeepAlivePeriod on TCPConn %v", err)
	}
}

func (n *Net) createConnection(ctx context.Context, address net.Addr, remotePub p2pcrypto.PublicKey, session NetworkSession) (ManagedConnection, error) {
	if n.isShuttingDown() {
		return nil, fmt.Errorf("can't dial because the connection is shutting down")
	}

	n.logger.Debug("Dialing %v @ %v...", remotePub.String(), address.String())
	netConn, err := n.dial(ctx, address)
	if err != nil {
		return nil, err
	}

	n.logger.Debug("Connected to %s...", address.String())
	return newConnection(netConn, n, remotePub, session, n.config.MsgSizeLimit, n.config.ResponseTimeout, n.logger), nil
}

func (n *Net) createSecuredConnection(ctx context.Context, address net.Addr, remotePubkey p2pcrypto.PublicKey) (ManagedConnection, error) {
	session := createSession(n.localNode.PrivateKey(), remotePubkey)
	conn, err := n.createConnection(ctx, address, remotePubkey, session)
	if err != nil {
		return nil, err
	}

	handshakeMessage, err := generateHandshakeMessage(session, n.networkID, n.listenAddress.Port, n.localNode.PublicKey())
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.Send(ctx, handshakeMessage); err != nil {
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
func (n *Net) Dial(ctx context.Context, address net.Addr, remotePubkey p2pcrypto.PublicKey) (Connection, error) {
	if n.listenAddress == nil {
		return nil, errors.New("net listenAddress must be set")
	}

	conn, err := n.createSecuredConnection(ctx, address, remotePubkey)
	if err != nil {
		return nil, fmt.Errorf("failed to dial, err: %v", err)
	}

	// Add session ID to context
	go conn.beginEventProcessing(log.WithNewSessionID(ctx,
		log.FieldNamed("session_remote_id", remotePubkey),
		log.String("session_remote_addr", address.String()),
		log.String("session_network", address.Network())))
	return conn, nil
}

// Shutdown initiate a graceful closing of the TCP listener and all other internal routines
func (n *Net) Shutdown() {
	if n.listener != nil {
		err := n.listener.Close()
		if err != nil {
			n.logger.With().Error("error closing listener", log.Err(err))
		}
	}
}

func (n *Net) isShuttingDown() bool {
	select {
	case <-n.shutdownCtx.Done():
		return true
	default:
	}
	return false
}

func (n *Net) accept(ctx context.Context, listen net.Listener, pending chan struct{}) {
	n.logger.Debug("waiting for incoming connections")

	for {
		// Grab a token to accept another incoming connection
		<-pending
		netConn, err := listen.Accept()
		if err != nil {
			if n.isShuttingDown() {
				return
			}
			if !Temporary(err) {
				n.logger.WithContext(ctx).With().Error("listener errored while accepting connections", log.Err(err))
				return
			}

			n.logger.WithContext(ctx).With().Warning("failed to accept connection request", log.Err(err))

			// Connection failed, replace the token
			pending <- struct{}{}
			continue
		}

		// Create a new session context to track this listener
		n.logger.WithContext(ctx).With().Debug("got new connection",
			log.String("remote_addr", netConn.RemoteAddr().String()),
			log.String("network", netConn.RemoteAddr().Network()))
		conn := netConn.(*net.TCPConn)
		n.tcpSocketConfig(conn) // TODO maybe only set this after session handshake to prevent denial of service with big messages
		c := newConnection(netConn, n, nil, nil, n.config.MsgSizeLimit, n.config.ResponseTimeout, n.logger.WithContext(ctx))

		// Handle the incoming connection asynchronously
		go n.acceptAsync(ctx, c, pending)
	}
}

func (n *Net) acceptAsync(ctx context.Context, conn fmtConnection, pending chan struct{}) {
	// Return the new connection token once the connection is closed
	defer func() { pending <- struct{}{} }()

	if err := conn.setupIncoming(ctx, n.config.SessionTimeout); err != nil {
		n.logger.WithContext(ctx).Event().Warning("incoming connection failed",
			log.String("remote_addr", conn.RemoteAddr().String()),
			log.Err(err))
		return
	}

	// The connection is automatically closed when there's no more to read, no need to close it here
	conn.beginEventProcessing(ctx)

	// network won't publish the connection before the remote node has established a session
}

// SubscribeOnNewRemoteConnections registers a callback for a new connection event. all registered callbacks are called before moving.
func (n *Net) SubscribeOnNewRemoteConnections(f func(event NewConnectionEvent)) {
	n.regMutex.Lock()
	n.regNewRemoteConn = append(n.regNewRemoteConn, f)
	n.regMutex.Unlock()
}

func (n *Net) publishNewRemoteConnectionEvent(conn Connection, node *node.Info) {
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

func verifyNetworkIDAndClientVersion(networkID uint32, handshakeData *HandshakeData) error {
	// compare that version to the min client version in config
	ok, err := version.CheckNodeVersion(handshakeData.ClientVersion, config.MinClientVersion)
	if err == nil && !ok {
		return errors.New("unsupported client version")
	}
	if err != nil {
		return fmt.Errorf("invalid client version, err: %v", err)
	}

	// make sure we're on the same network
	if handshakeData.NetworkID != networkID {
		return fmt.Errorf("request net id (%d) is different than local net id (%d)", handshakeData.NetworkID, networkID)
		//TODO : drop and blacklist this sender
	}
	return nil
}

func generateHandshakeMessage(session NetworkSession, networkID uint32, localIncomingPort int, localPubkey p2pcrypto.PublicKey) ([]byte, error) {
	handshakeData := &HandshakeData{
		ClientVersion: config.ClientVersion,
		NetworkID:     networkID,
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
