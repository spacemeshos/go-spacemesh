package p2p2

import "github.com/UnrulyOS/go-unruly/log"

// Remote node data
// Remote nodes are maintained by the swarm and are not visible to higher-level types on the network stack
// All remote node methods are NOT thread-safe - they are designed to be used only from a singleton swarm object

// Remode node is designed to handle remote node specific ops by the swarm
type RemoteNode interface {
	Id() []byte     // node id is public key bytes
	String() string // node public key string
	Pretty() string

	TcpAddress() string // tcp address advertised by node e.g. 127.0.0.1:3058

	PublicKey() PublicKey

	GetConnections() map[string]Connection
	GetSessions() map[string]NetworkSession

	// returns an authenticated session with the node if one exists
	GetAuthenticatedSession() NetworkSession

	// returns an active connection with the node if we have one
	GetActiveConnection() Connection

	// add a queue of pending messages to send to the node - can be picked once a session is created

}

type remoteNodeImpl struct {
	publicKey  PublicKey
	tcpAddress string

	connections map[string]Connection
	sessions    map[string]NetworkSession
}

// Create a new remote node using provided id and tcp address
func NewRemoteNode(id string, tcpAddress string) (RemoteNode, error) {

	key, err := NewPublicKeyFromString(id)
	if err != nil {
		log.Error("invalid node id format: %v", err)
		return nil, err
	}

	node := &remoteNodeImpl{
		publicKey:   key,
		tcpAddress:  tcpAddress,
		connections: make(map[string]Connection),
		sessions:    make(map[string]NetworkSession),
	}

	return node, nil
}

func (n *remoteNodeImpl) GetAuthenticatedSession() NetworkSession {
	for _, v := range n.sessions {
		if v.IsAuthenticated() {
			return v
		}
	}
	return nil
}

func (n *remoteNodeImpl) GetActiveConnection() Connection {

	// todo: sort by last data transfer time to pick the best connection

	// just return a random connection for now
	for _, v := range n.connections {
		return v
	}

	return nil
}

func (n *remoteNodeImpl) GetConnections() map[string]Connection {
	return n.connections
}

func (n *remoteNodeImpl) GetSessions() map[string]NetworkSession {
	return n.sessions
}

func (n *remoteNodeImpl) String() string {
	return n.publicKey.String()
}

func (n *remoteNodeImpl) Id() []byte {
	return n.publicKey.Bytes()
}

func (n *remoteNodeImpl) Pretty() string {
	return n.publicKey.Pretty()
}

func (n *remoteNodeImpl) PublicKey() PublicKey {
	return n.publicKey
}

func (n *remoteNodeImpl) TcpAddress() string {
	return n.tcpAddress
}
