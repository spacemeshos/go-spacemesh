package p2p

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

// A network peer known to the local node. At minimum local node knows its id (public key) and tcp address.
// Peers are maintained by the swarm and are not visible to higher-level types on the network stack
// All Peer methods are NOT thread-safe - they are designed to be used only from a singleton swarm object
// Peer handles swarm sessions and net connections with a remote node
type Peer interface {
	Id() []byte     // node id is public key bytes
	String() string // node public key string
	Pretty() string
	TcpAddress() string // tcp address advertised by node e.g. 127.0.0.1:3058
	PublicKey() crypto.PublicKey
	GetConnections() map[string]net.Connection

	DeleteAllConnections()

	GetSessions() map[string]NetworkSession

	// returns an authenticated session with the node if one exists
	GetAuthenticatedSession() NetworkSession

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

// Create a new remote node using provided id and tcp address
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

func (n *peerImpl) DeleteAllConnections() {
	n.connections = make(map[string]net.Connection)
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
