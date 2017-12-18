package p2p

import (
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p/nodeconfig"
	"github.com/UnrulyOS/go-unruly/p2p/pb"
	"github.com/gogo/protobuf/proto"
)

// The local unruly node is the root of all evil
type LocalNode interface {
	Id() []byte
	String() string
	Pretty() string

	PrivateKey() crypto.PrivateKey
	PublicKey() crypto.PublicKey

	TcpAddress() string
	NewProtocolMessageMetadata(protocol string, reqId []byte, gossip bool) *pb.Metadata
	Sign(data proto.Message) ([]byte, error)
	SignToString(data proto.Message) (string, error)

	GetSwarm() Swarm
	GetPing() Ping

	SendMessage(req SendMessageReq)

	Shutdown()

	Config() nodeconfig.Config
}

// Create a local node with a provided ip address
func NewLocalNode(tcpAddress string, config nodeconfig.Config) (LocalNode, error) {

	if len(nodeconfig.ConfigValues.NodeId) > 0 {
		// user provided node id/pubkey via cli - attempt to start that node w persisted data
		data := readNodeData(nodeconfig.ConfigValues.NodeId)
		return newNodeFromData(tcpAddress, data, config)
	}

	// look for persisted node data in the nodes directory
	// load the node with the data of the first node found
	nodeData := readFirstNodeData()
	if nodeData != nil {
		// crete node using persisted node data
		return newNodeFromData(tcpAddress, nodeData, config)
	}

	// generate new node
	return newNodeIdentity(tcpAddress, config)
}

func newNodeIdentity(tcpAddress string, config nodeconfig.Config) (LocalNode, error) {
	priv, pub, _ := crypto.GenerateKeyPair()
	return NewLocalNodeWithKeys(pub, priv, tcpAddress, config)
}

func NewLocalNodeWithKeys(pubKey crypto.PublicKey, privKey crypto.PrivateKey, tcpAddress string, config nodeconfig.Config) (LocalNode, error) {

	n := &localNodeImp{
		pubKey:     pubKey,
		privKey:    privKey,
		tcpAddress: tcpAddress,
		config: config,
	}

	// swarm owned by node
	s, err := NewSwarm(tcpAddress, n)
	if err != nil {
		log.Error("can't create a local node without a swarm. %v", err)
		return nil, err
	}


	n.swarm = s
	n.ping = NewPingProtocol(s)

	// todo: fix this - file access issues

	err = n.persistData()
	if err != nil { // no much use of starting if we can't store node private key in store
		log.Error("Failed to persist node data to local store: %v", err)
		return nil, err
	}

	return n, nil
}

func newNodeFromData(tcpAddress string, d *NodeData, config nodeconfig.Config) (LocalNode, error) {
	priv := crypto.NewPrivateKeyFromString(d.PrivKey)
	pub, err := crypto.NewPublicKeyFromString(d.PubKey)
	if err != nil {
		log.Error("Failded to create public key from string: %v", err)
		return nil, err
	}

	return NewLocalNodeWithKeys(pub, priv, tcpAddress, config)
}
