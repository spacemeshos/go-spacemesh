package p2p2

// Swarm
// Hold ref to peers and manage connections to peers

// p2p2 Stack:
// -----------
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
//  ....

type Swarm interface {

	// attempt to establish a session with a remote node
	ConnectTo(req NodeReq)

	// forcefully disconnect form a node
	DisconnectFrom(req NodeReq)

	// high level API - used by muxer - sends a messge and gets a callback with data or error (async)
	// Should only be called by muxer
	SendMessage(SendMessageReqParams)

	// todo: Register muxer to handle incoming messages to higher level protocols and handshake protocol
}

type SendMessgeResp struct {
	responder RemoteNode
	reqId string
	msg	[]byte
	err error
}

type SendMessageReqParams struct {
	dest RemoteNode
	reqId string
	msg      []byte
	callback chan SendMessgeResp
}

type NodeReq struct {
	node RemoteNode
	callback chan NodeResp
}

type NodeResp struct {
	node RemoteNode
	err error
}

type swarmImpl struct {

	// set in construction and immutable state
	network   Network
	localNode LocalNode

	// all data should only be accessed from methods executed by the main swarm event loop

	// Internal state not thread safe state - must be access only from methods dispatched from the internal event handler
	peers             map[string]RemoteNode // remote known nodes mapped by their ids (keys) - Swarm is a peer store. NodeId -> RemoteNode
	connections       map[string]Connection // all open connections to nodes by conn id. ConnId -> Con.
	peersByConnection map[string]RemoteNode // remote nodes indexed by their connections. ConnId -> RemoteNode

	// add registered callbacks in a sync.map to return to the muxer responses to outgoing messages

	// comm channels
	connectionRequests chan NodeReq        // request to establish a session w a remote node
	disconnectRequests chan NodeReq        // kill session and disconnect from node
	sendMsgRequests    chan SendMessageReqParams // send a message to a node and callback on error or data
	kill               chan bool              // used to kill the swamp from outside. e.g when local node is shutting down

}

func NewSwarm(tcpAddress string, l LocalNode) (Swarm, error) {

	n, err := NewNetwork(tcpAddress)
	if err != nil {
		return nil, err
	}

	s := &swarmImpl{
		localNode:          l,
		network:            n,
		kill:               make(chan bool),
		peersByConnection:  make(map[string]RemoteNode),
		peers:              make(map[string]RemoteNode),
		connections:        make(map[string]Connection),
		connectionRequests: make(chan NodeReq, 10),
		disconnectRequests: make(chan NodeReq, 10),
		sendMsgRequests:    make(chan SendMessageReqParams, 20),
	}

	go s.beginProcessingEvents()

	return s, err
}

func (s *swarmImpl) ConnectTo(req NodeReq) {
	s.connectionRequests <- req
}

func (s *swarmImpl) DisconnectFrom(req NodeReq) {
	s.disconnectRequests <- req
}

// Send a message to a remote node
// Impl will establish session if needed or use an existing session and open connection
// This should be called by the muxer
func (s *swarmImpl) SendMessage(req SendMessageReqParams) {
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

func (s *swarmImpl) onConnectionRequest(req NodeReq) {
	// connect to node...
}

func (s *swarmImpl) onDisconnectionRequest(req NodeReq) {
	// disconnect from node...
}

func (s *swarmImpl) onMessgeSenderRequest(p SendMessageReqParams) {
	// todo: send message here - establish a connection and session on-demand as needed
	// todo: auto support for retries
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
