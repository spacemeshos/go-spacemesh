package p2p2

import ()

// Bare-bones remote node data. Used for bootstrap node
// Implements handshake protocol?
// Should be internal type to p2p2 - used by Swarm
type RemoteNode interface {
	Id() []byte     // node id is public key bytes
	String() string // node public key string
	Pretty() string
	TcpAddress() string // tcp address advertised by node e.g. 127.0.0.1:3058 - todo consider multiaddress here

	PublicKey() PublicKey

	// send a message to the remote node with optional callback
	//SendMessage(data []byte, callback func(node RemoteNode, data []byte))
}

type RemoteNodeImpl struct {
	publicKey  PublicKey
	tcpAddress string
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
