package p2p2

// Swarm
// Hold ref to peers and manage connections to peers

// p2p2 Stack:
// ----------------
// Local Node
// -- Protocols: impls with req/resp support (match id resp w request id)
// Muxer (node aspect) - routes remote requests (and responses) back to protocols. Protocols register on muxer
// Swarm forward messages to muxer
// Swarm - managed all remote nodes, sessions and connections
// Remote Node - maintains sessions and connections
// Connection
// -- NetworkSession (optional)
// -- msgio (prefix length encoding)
// Network
//	- tcp ip

type Swarm interface {

	// attempt to establish a session with a remote node
	ConnectTo(node RemoteNode)
	DisconnectFrom(node RemoteNode)

	// high level API - used by protocols - send a message to remote node
	// this is below the protocol level - used by protocol muxer
	SendMessage(SendMessageParams)

	// Register muxer to handle incoming messages to higher level protocols
}

type SendMessageParams struct {
	msg []byte
	callback func(msg []byte, err error)
}

type swarmImpl struct {
	network   Network
	localNode LocalNode

	kill chan bool // used to kill the swamp from outside in

	// all data should only be accessed from methods executed by the main swarm event loop

	// not thread safe state - must be access only from methods dispatched from the internal event handler
	peers            map[string]RemoteNode // remote known nodes mapped by their ids (keys) - Swarm is a peer store. NodeId -> RemoteNode
	connections      map[string]Connection // all open connections to nodes by conn id. ConnId -> Con.
	peersByConnection map[string]RemoteNode // remote nodes indexed by their connections. ConnId -> RemoteNode

	// add registered callbacks in a sync.map to return to the muxer responses to outgoing messages

	connectionRequests chan RemoteNode
	disconnectRequests chan RemoteNode
	sendMsgRequests chan SendMessageParams
}

func NewSwarm(tcpAddress string, l LocalNode) (Swarm, error) {

	n, err := NewNetwork(tcpAddress)
	if err != nil {
		return nil, err
	}

	s := &swarmImpl{
		localNode:        l,
		network:          n,
		kill:             make(chan bool),
		peersByConnection: make(map[string]RemoteNode),
		peers:            make(map[string]RemoteNode),
		connections:      make(map[string]Connection),
		connectionRequests: make(chan RemoteNode, 10),
		disconnectRequests: make (chan RemoteNode, 10),
		sendMsgRequests: make(chan SendMessageParams, 20),
	}

	go s.beginProcessingEvents()

	return s, err
}

func (s *swarmImpl) ConnectTo(node RemoteNode) {
	s.connectionRequests <- node
}

func (s *swarmImpl) DisconnectFrom(node RemoteNode) {
	s.disconnectRequests <- node
}

// Send a message to a remote node
// Impl will establish session if needed or use an existing session and open connection
// This should be called by the muxer
func (s *swarmImpl) SendMessage(req SendMessageParams) {
	s.sendMsgRequests <- req
}

// Swarm serial event processing
// provides concurrency safety as only one callback is executed at a time
// so there's no need for sync internal data structures
func (s *swarmImpl) beginProcessingEvents() {

Loop:
	for {
		select {
		case <-s.kill:
			// todo: gracefully stop the swarm - close all connections to remote nodes
			break Loop

		case c := <-s.network.GetNewConnections():
			s.onRemoteClientConnected(c)

		case m := <-s.network.GetIncomingMessage():
			s.onRemoteClientMessage(m)

		case err := <-s.network.GetConnectionErrors():
			s.onConnectionError(err)

		case err := <-s.network.GetMessageSendErrors():
			s.onMessageSendError(err)

		case c := <-s.network.GetClosingConnections():
			s.onConnectionClosed(c)


		case r := <-s.sendMsgRequests:
			s.onMessgeSenderRequest(r)


		case n := <-s.connectionRequests:
			s.onConnectionRequest(n)


		case n := <-s.disconnectRequests:
			s.onDisconnectionRequest(n)
		}
	}
}

func (s *swarmImpl) onConnectionRequest(n RemoteNode) {
	// connect to node...
}

func (s *swarmImpl) onDisconnectionRequest(n RemoteNode) {
	// disconnect from node...
}

func (s *swarmImpl) onMessgeSenderRequest(p SendMessageParams) {
	// todo: send message here
}

// not go safe - called from event processing main loop
func (s *swarmImpl) onConnectionClosed(c Connection) {
	delete(s.connections, c.Id())
}

// not go safe - called from event processing main loop
func (s *swarmImpl) onRemoteClientConnected(c Connection) {
	// nop - a remote client connected

}

// Main network messages handler
// c: connection we got this message on
// msg: binary protobufs encoded data
// not go safe - called from event processing main loop
func (s *swarmImpl) onRemoteClientMessage(msg ConnectionMessage) {

	// got a message from a remote client - process it here

	// decyrpt protobuf - all messages are protobufs

	// auth message sent by remote node id and close connection otherwise

	// create remote node if needed

	// create session with remote node if needed - handle here - implement handshake protocol

	// if session exists and this is a session message - multiplexing back to calling protocol handler with remoteNode
	// just send to muxer
}

// not go safe - called from event processing main loop
func (s *swarmImpl) onConnectionError(err ConnectionError) {
	// close the connection?
	// who to notify?
}

// not go safe - called from event processing main loop
func (s *swarmImpl) onMessageSendError(err MessageSendError) {
	// retry ?
}

// todo: handshake protocol
