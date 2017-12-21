package p2p

import (
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p/net"
	"github.com/UnrulyOS/go-unruly/p2p/node"
)

// A remote node
// Remote nodes are maintained by the swarm and are not visible to higher-level types on the network stack
// All remote node methods are NOT thread-safe - they are designed to be used only from a singleton swarm object

// Peer maintains swarm sessions and connections with a remote node
type Peer interface {
	Id() []byte     // node id is public key bytes
	String() string // node public key string
	Pretty() string

	TcpAddress() string // tcp address advertised by node e.g. 127.0.0.1:3058

	PublicKey() crypto.PublicKey

	GetConnections() map[string]net.Connection
	GetSessions() map[string]NetworkSession

	// returns an authenticated session with the node if one exists
	GetAuthenticatedSession() NetworkSession

	// returns an active connection with the node if we have one
	GetActiveConnection() net.Connection

	GetRemoteNodeData() node.RemoteNodeData
}

type peerImpl struct {
	publicKey  crypto.PublicKey
	tcpAddress string

	connections map[string]net.Connection
	sessions    map[string]NetworkSession
}

// Create a new remote node using provided id and tcp address
func NewRemoteNode(id string, tcpAddress string) (Peer, error) {

	key, err := crypto.NewPublicKeyFromString(id)
	if err != nil {
		log.Error("invalid node id format: %v", err)
		return nil, err
	}

	node := &peerImpl{
		publicKey:   key,
		tcpAddress:  tcpAddress,
		connections: make(map[string]net.Connection),
		sessions:    make(map[string]NetworkSession),
	}

	return node, nil
}

func (n *peerImpl) GetAuthenticatedSession() NetworkSession {
	for _, v := range n.sessions {
		if v.IsAuthenticated() {
			return v
		}
	}
	return nil
}

func (n *peerImpl) GetRemoteNodeData() node.RemoteNodeData {
	return node.NewRemoteNodeData(n.String(), n.TcpAddress())
}

func (n *peerImpl) GetActiveConnection() net.Connection {

	// todo: sort by last data transfer time to pick the best connection

	// just return a random connection for now
	for _, v := range n.connections {
		return v
	}

	return nil
}

func (n *peerImpl) GetConnections() map[string]net.Connection {
	return n.connections
}

func (n *peerImpl) GetSessions() map[string]NetworkSession {
	return n.sessions
}

func (n *peerImpl) String() string {
	return n.publicKey.String()
}

func (n *peerImpl) Id() []byte {
	return n.publicKey.Bytes()
}

func (n *peerImpl) Pretty() string {
	return n.publicKey.Pretty()
}

func (n *peerImpl) PublicKey() crypto.PublicKey {
	return n.publicKey
}

func (n *peerImpl) TcpAddress() string {
	return n.tcpAddress
}
