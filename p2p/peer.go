package p2p

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

// Peer is a remote network node.
// At minimum local node knows its id (public key) and announced tcp address/port.
// Peers are maintained by the swarm and are not visible to higher-level types on the network stack
// All Peer methods are NOT thread-safe - they are designed to be used only from a singleton Swarm type
// Peer handles swarm sessions and net connections with a remote node
type Peer interface {
	ID() []byte     // node id is public key bytes
	String() string // node public key string
	Pretty() string
	TCPAddress() string // tcp address advertised by node e.g. 127.0.0.1:3058
	PublicKey() crypto.PublicKey
	GetConnections() map[string]net.Connection

	DeleteAllConnections()

	GetSessions() map[string]NetworkSession

	// returns an authenticated session with the node if one exists
	GetLastSession() NetworkSession

	// returns an active connection with the node if we have one
	GetActiveConnection() net.Connection

	// returns RemoteNodeData for this peer
	GetRemoteNodeData() node.RemoteNodeData
}

type peerImpl struct {
	publicKey  crypto.PublicKey
	tcpAddress string

	connections map[string]net.Connection
	sessions    map[string]NetworkSession
}

// NewRemoteNode creates a new remote node using provided id and tcp address.
func NewRemoteNode(id string, tcpAddress string) (Peer, error) {

	key, err := crypto.NewPublicKeyFromString(id)
	if err != nil {
		log.Error("invalid node id format", err)
		return nil, err
	}

	n := &peerImpl{
		publicKey:   key,
		tcpAddress:  tcpAddress,
		connections: make(map[string]net.Connection),
		sessions:    make(map[string]NetworkSession),
	}

	return n, nil
}

func (n *peerImpl) GetLastSession() NetworkSession {
	var lastCreated NetworkSession = nil
	for _, v := range n.sessions {
		if lastCreated == nil || v.Created().After(lastCreated.Created()) {
			lastCreated = v
		}
	}
	return lastCreated
}

func (n *peerImpl) GetRemoteNodeData() node.RemoteNodeData {
	return node.NewRemoteNodeData(n.String(), n.TCPAddress())
}

func (n *peerImpl) GetActiveConnection() net.Connection {

	// todo: sort by last data transfer time to pick the best connection

	// just return a random connection for now
	for _, v := range n.connections {
		return v
	}

	return nil
}

// GetConnections gets all connections with this peer.
func (n *peerImpl) GetConnections() map[string]net.Connection {
	return n.connections
}

// DeleteAllConnections delete all connections with this peer.
func (n *peerImpl) DeleteAllConnections() {
	n.connections = make(map[string]net.Connection)
}

// GetSession returns all the network sessions with this peer.
func (n *peerImpl) GetSessions() map[string]NetworkSession {
	return n.sessions
}

// String returns a string identifier for this peer.
func (n *peerImpl) String() string {
	return n.publicKey.String()
}

// ID returns the binary identifier for this peer.
func (n *peerImpl) ID() []byte {
	return n.publicKey.Bytes()
}

// Pretty returns a readable identifier for this peer.
func (n *peerImpl) Pretty() string {
	return n.publicKey.Pretty()
}

// PublicKey returns the peer's public key.
func (n *peerImpl) PublicKey() crypto.PublicKey {
	return n.publicKey
}

// TCPAddress returns the public TCP address of this peer.
func (n *peerImpl) TCPAddress() string {
	return n.tcpAddress
}
