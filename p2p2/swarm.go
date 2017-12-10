package p2p2

import (
	"time"
)

// Session info with a remote node - wraps connection
type NetworkSession interface {
	Connection          // session's connection
	key() []byte        // session key
	Created() time.Time // time when session was established
}

// Swarm
// Hold ref to peers and manage connections to peers

// p2p Stack:
// Local Node
// -- implements protocols with req/resp support
// Swarm
// Remote Node
// NetworkSession
// Connection
// Network

type Swarm interface {
}

type swarmImpl struct {
	network   Network
	localNode LocalNode

	kill chan bool // used to kill the swamp from outside in

	NewRemoteNodeChan    chan RemoteNode // push new remote nodes here
	NewConnectionChan    chan Connection // push new connections here
	closedConnectionChan chan Connection

	remoteNodes      map[string]RemoteNode // remote known nodes mapped by their ids (keys)
	connections      map[string]Connection // all open connections to nodes by conn id
	nodesByConection map[string]RemoteNode // remote nodes indexed by their connections
}

// todo: add local node as param
func NewSwarm(tcpAddress string, localNode LocalNode) (Swarm, error) {

	n, err := NewNetwork(tcpAddress)
	if err != nil {
		return nil, err
	}

	s := &swarmImpl{
		localNode:            localNode,
		network:              n,
		kill:                 make(chan bool),
		NewRemoteNodeChan:    make(chan RemoteNode, 10),
		NewConnectionChan:    make(chan Connection, 10),
		closedConnectionChan: make(chan Connection, 10),
		nodesByConection:     make(map[string]RemoteNode),
		remoteNodes:          make(map[string]RemoteNode),
		connections:          make(map[string]Connection),
	}

	go s.beginProcessingEvents()

	return s, err
}

// serial event processing
func (s *swarmImpl) beginProcessingEvents() {

	// setup network callbacks
	s.network.OnRemoteClientConnected(s.onRemoteClientConnected)
	s.network.OnRemoteClientMessage(s.onRemoteClientMessage)
	s.network.OnMessageSendError(s.onMessageSendError)
	s.network.OnConnectionError(s.onConnectionError)
	s.network.OnRemoteClientMessage(s.onRemoteClientMessage)

Loop:
	for {
		select {
		case <-s.kill:
			// todo: gracefully stop the swarm - close all connections to remote nodes
			break Loop
		case node := <-s.NewRemoteNodeChan:
			s.remoteNodes[string(node.Id())] = node
		case c := <-s.NewConnectionChan:
			s.connections[string(c.Id())] = c
		case c := <-s.closedConnectionChan:
			s.connectionClosed(c)
		}
	}
}

// go safe - called from event processing
func (s *swarmImpl) connectionClosed(c Connection) {

	node := s.nodesByConection[c.Id()]
	if node != nil {
		node.RemoveConnection(c.Id())
	}

	delete(s.connections, c.Id())
}

func (s *swarmImpl) onRemoteClientConnected(c Connection) {
	// nop - a remote client connected

}

func (s *swarmImpl) onConnectionClosed(c Connection) {
	s.closedConnectionChan <- c
}

func (s *swarmImpl) onRemoteClientMessage(c Connection, message []byte) {

	// got a message from a remote client - process it here

	// create remote node if needed

	// create session with remote node if needed - handle here - implement handshake protocol

	// if session exists and this is a session message - multiplexing back to calling protocol handler with remoteNode
}

func (s *swarmImpl) onConnectionError(c Connection, err error) {
	// close the connection?
	// who to notify?
}

func (s *swarmImpl) onMessageSendError(c Connection, err error) {
	// retry ?
}

// todo: handshake protocol
