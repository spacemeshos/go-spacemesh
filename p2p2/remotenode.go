package p2p2

import (
	"github.com/UnrulyOS/go-unruly/log"
	"sync"
)

type LocalNode interface {
	Id() []byte
	String() string
	Pretty() string

	PrivateKey() PrivateKey
	PublicKey() PublicKey
}

// Bare-bones remote node data. Used for bootstrap node
// Implements handshake protocol?
type RemoteNode interface {
	Id() []byte     // node id is public key bytes
	String() string // node public key string
	Pretty() string
	TcpAddress() string // tcp address advertised by node e.g. 127.0.0.1:3058 - todo consider multiaddress here

	GetSessions() []NetworkSession // 0 or more active sessions of local node with the remote node
	GetConnections() []Connection  // 0 or more network non-authenticated connections that we don't have an established session for yet

	AddConnection(c Connection)
	AddSession(s NetworkSession)

	RemoveConnection(connId string)

	PublicKey() PublicKey

	// send a message to the remote node with optional callback
	//SendMessage(data []byte, callback func(node RemoteNode, data []byte))
}

type RemoteNodeImpl struct {
	publicKey  PublicKey
	tcpAddress string

	sessions    sync.Map // zero value is empty map
	connections sync.Map // zero value is empty map
}

// Create a new remote node using provided id and tcp address
func NewRemoteNode(id string, tcpAddress string) (RemoteNode, error) {

	key, err := NewPublicKeyFromString(id)
	if err != nil {
		return nil, err
	}

	node := &RemoteNodeImpl{
		publicKey:  key,
		tcpAddress: tcpAddress,
	}

	return node, nil
}

func (n *RemoteNodeImpl) SendMessage(data []byte, callback func(data []byte)) {
	//
}

func (n *RemoteNodeImpl) GetConnections() []Connection {
	cons := make([]Connection, 1)
	n.connections.Range(func(key, value interface{}) bool {
		c, ok := value.(Connection)
		if !ok {
			log.Error("Expected map to hold only Connections")
			return true
		}

		cons = append(cons, c)
		return true
	})
	return cons
}

func (n *RemoteNodeImpl) GetSessions() []NetworkSession {
	s := make([]NetworkSession, 1)
	n.sessions.Range(func(key, value interface{}) bool {
		session, ok := value.(NetworkSession)
		if !ok {
			log.Error("Expected map to hold only NeworkSessions")
			return true
		}

		s = append(s, session)
		return true
	})
	return s
}

func (n *RemoteNodeImpl) AddSession(s NetworkSession) {
	n.sessions.Store(s.Id(), s)
}

func (n *RemoteNodeImpl) AddConnection(c Connection) {
	n.connections.Store(c.Id(), c)
}

func (n *RemoteNodeImpl) RemoveConnection(connId string) {
	n.sessions.Delete(connId)
	n.connections.Delete(connId)
}

func (n *RemoteNodeImpl) Id() []byte {
	return n.publicKey.Bytes()
}

func (n *RemoteNodeImpl) String() string {
	return n.publicKey.String()
}

func (n *RemoteNodeImpl) Pretty() string {
	return n.publicKey.Pretty()
}

func (n *RemoteNodeImpl) PublicKey() PublicKey {
	return n.publicKey
}

func (n *RemoteNodeImpl) TcpAddress() string {
	return n.tcpAddress
}
