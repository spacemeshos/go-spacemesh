package node

import (
	"context"
	"fmt"
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/node/config"
	"gx/ipfs/QmRS46AyqtpJBsf1zmQdeizSDEzo1qkWR7rdEuPFAv8237/go-libp2p-host"

	bhost "github.com/libp2p/go-libp2p/p2p/host/basic"
	ps "gx/ipfs/QmPgDWmTmuzvP7QE5zwo1TmjbJme9pmZHNujB2453jkCTr/go-libp2p-peerstore"
	swarm "gx/ipfs/QmU219N3jn7QadVCeBUqGnAkwoXoUomrCwDuVQVuL7PB5W/go-libp2p-swarm"
	ma "gx/ipfs/QmXY77cVe7rVRQXZZQRioukUM7aRW3BTcAgJe12MCtb3Ji/go-multiaddr"

	libp2pcrypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"
)

// Node type - a local p2p host implementing one or more p2p protocols
type Node struct {
	host.Host     // lib-p2p host (interface)
	*PingProtocol // ping protocol impl
	*EchoProtocol // echo protocol impl
	// add other protocols here...

	// Node crypto ids and keys (interfaces)
	publicKey  crypto.PublicKeylike
	privateKey crypto.PrivateKeylike
	identity   crypto.Identifier
}

// Persisted node data
type NodeData struct {
	Id      string
	PubKey  string
	PrivKey string
}

// Create a new local node with its implemented protocols and persist its data to store
func newNode(host host.Host, done chan bool, pubKey crypto.PublicKeylike, privKey crypto.PrivateKeylike, id crypto.Identifier) *Node {

	n := &Node{Host: host, publicKey: pubKey, privateKey: privKey, identity: id}
	n.PingProtocol = NewPingProtocol(n, done)
	n.EchoProtocol = NewEchoProtocol(n, done)

	// persist node data to store and panic otherwise
	// there's little use of running a node without being able to restore it in future app sessions
	err := n.persistData()

	if err != nil {
		log.Error("Failed to persist node data for node id: %s. %v", n.identity.String(), err)
		panic(err)
	}

	return n
}

// Create a new node using persisted node data
// Generates a new node (new id, pub, priv keys) if needed
func NewNode(port uint, done chan bool) *Node {

	// user provided node id via cli - attempt to start node
	// using persisted data for this node id
	if len(config.ConfigValues.NodeId) > 0 {
		data := readNodeData(config.ConfigValues.NodeId)
		return newNodeFromData(port, done, data)
	}

	// look for node data in the nodes directory
	// load the node with the data of the first node found
	nodeData := readFirstNodeData()
	if nodeData != nil {
		// crete node using persisted node data
		return newNodeFromData(port, done, nodeData)
	}

	// We have no persistent node data - generate a new node
	return NewNodeIdentity(port, done)
}

// Create a new node from node data
func newNodeFromData(port uint, done chan bool, nodeData *NodeData) *Node {

	priv, err := crypto.NewPrivateKeyFromString(nodeData.PrivKey)
	if err != nil {
		log.Error("Failded to create private key from string: %v", err)
		return nil
	}

	pub, err := crypto.NewPublicKeyFromString(nodeData.PubKey)
	if err != nil {
		log.Error("Failded to create public key from string: %v", err)
		return nil
	}

	id, err := crypto.NewIdentifier(nodeData.Id)
	if err != nil {
		log.Error("Failded to create id from string: %v", err)
		return nil
	}

	log.Info("Creating node from persisted data for node id: %s", nodeData.Id)

	return newLocalNodeWithKeys(port, done, priv, pub, id)
}

// Create a new node with new crypto keys and id
func NewNodeIdentity(port uint, done chan bool) *Node {
	priv, pub, _ := crypto.GenerateKeyPair(libp2pcrypto.Secp256k1, 256)
	id, _ := pub.IdFromPubKey()
	log.Info("Creating new node with id: %s", id.String())
	return newLocalNodeWithKeys(port, done, priv, pub, id)
}

// Create a new local node
func newLocalNodeWithKeys(port uint, done chan bool, priv crypto.PrivateKeylike, pub crypto.PublicKeylike, id crypto.Identifier) *Node {
	listen, _ := ma.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port))
	peerStore := ps.NewPeerstore()
	peerStore.AddPrivKey(id.PeerId(), priv.PrivatePeerKey())
	peerStore.AddPubKey(id.PeerId(), pub.PublicPeerKey())
	n, _ := swarm.NewNetwork(context.Background(), []ma.Multiaddr{listen}, id.PeerId(), peerStore, nil)
	aHost := bhost.New(n)

	//peerStore.AddAddrs(pid.ID, host.Addrs(), ps.PermanentAddrTTL)
	log.Info("local node started on tcp port: %d", port)

	node := newNode(aHost, done, pub, priv, id)

	// node startup tasks....
	node.loadBootstrapNodes()

	return node
}
