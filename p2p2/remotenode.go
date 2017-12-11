package p2p2

import (

)

// Bare-bones remote node data. Used for bootstrap node
// Implements handshake protocol?
// Should be internal type to p2p2 - used by Swarm
type RemoteNode interface {
	Id() []byte     // node id is public key bytes
	String() string // node public key string
	Pretty() string
	TcpAddress() string // tcp address advertised by node e.g. 127.0.0.1:3058 - todo consider multiaddress here

	GetConnections() []Connection // 0 or more network non-authenticated connections that we don't have an established session for yet

	AddConnection(Connection)
	RemoveConnection(id string)
	Disconnect()

	PublicKey() PublicKey

	// send a message to the remote node with optional callback
	//SendMessage(data []byte, callback func(node RemoteNode, data []byte))
}

type RemoteNodeImpl struct {
	publicKey   PublicKey
	tcpAddress  string
	connections map[string]Connection

	addConnection        chan Connection
	removeConnectionById chan string

	disconnect chan bool
}

// Create a new remote node using provided id and tcp address
func NewRemoteNode(id string, tcpAddress string) (RemoteNode, error) {

	key, err := NewPublicKeyFromString(id)
	if err != nil {
		return nil, err
	}

	node := &RemoteNodeImpl{
		publicKey:            key,
		tcpAddress:           tcpAddress,
		addConnection:        make(chan Connection, 5),
		removeConnectionById: make(chan string, 2),
		connections:          make(map[string]Connection),
		disconnect:           make(chan bool),
	}

	go node.processEvents()

	return node, nil
}

func (n *RemoteNodeImpl) GetConnections() []Connection {

	// todo: this is not thread safe as n.connections may change while we
	// are iterating over it - find a better solution to this
	// we don't want to make connections sync.map as all other processing is done via channels in a thread-safe manner
	conns := make([]Connection,1)
	for _, v := range n.connections {
		conns = append(conns, v)
	}

	return conns
}


func (n *RemoteNodeImpl) AddConnection(c Connection)  {
	n.addConnection <- c
}

func (n *RemoteNodeImpl) RemoveConnection(id string) {
	n.removeConnectionById <- id
}

func (n *RemoteNodeImpl) Disconnect() {
	n.disconnect <- true
}

func (n *RemoteNodeImpl) processEvents() {

Loop:
	for {
		select {
		case <-n.disconnect:
			break Loop

		case c := <-n.addConnection:
			n.connections[c.Id()] = c

		case id := <-n.removeConnectionById:
			delete(n.connections, id)

		}

	}
}

func (n *RemoteNodeImpl) SendMessage(data []byte, callback func(data []byte)) {
	//
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
