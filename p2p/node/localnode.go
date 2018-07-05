package node

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"sync"
)

// LocalNode implementation.
type LocalNode struct {
	Node
	privKey       crypto.PrivateKey
	pubTCPAddress string
	pubTCPMutex   sync.RWMutex

	networkID nodeconfig.NetworkID

	log.Log
}

// NetworkID returns the local node's network id (testnet/mainnet, etc..)
func (n *LocalNode) NetworkID() nodeconfig.NetworkID {
	return n.networkID
}

// PubTCPAddress returns the node's public tcp address.
func (n *LocalNode) PubTCPAddress() string {
	n.pubTCPMutex.RLock()
	addr := n.pubTCPAddress
	n.pubTCPMutex.RUnlock()
	return addr
}

// RefreshPubTCPAddress attempts to refresh the node public ip address and returns true if it was able to do so.
func (n *LocalNode) RefreshPubTCPAddress() bool {

	// Figure out node public ip address
	addr, err := net.GetPublicIPAddress()
	if err != nil {
		log.Error("failed to obtain public ip address")
		return false
	}

	port, err := net.GetPort(n.Address())
	if err != nil {
		log.Error("Invalid tcp ip address", err)
		return false
	}
	n.pubTCPMutex.Lock()
	n.pubTCPAddress = fmt.Sprintf("%s:%s", addr, port)
	n.pubTCPMutex.Unlock()
	return true
}

// PrivateKey returns this node's private key.
func (n *LocalNode) PrivateKey() crypto.PrivateKey {
	return n.privKey
}

// NewLocalNode creates a local node with a provided ip address.
// Attempts to set node node from persisted data in local store.
// Creates a new node if none was loaded.
func NewLocalNode(config nodeconfig.Config, address string, persist bool) (*LocalNode, error) {

	if len(config.NodeID) > 0 {
		// user provided node id/pubkey via the cli - attempt to start that node w persisted data
		data, err := readNodeData(config.NodeID)
		if err != nil {
			return nil, err
		}

		return newLocalNodeFromFile(address, data, persist)
	}

	// look for persisted node data in the nodes directory
	// load the node with the data of the first node found
	nodeData, err := readFirstNodeData()
	if err != nil {
		log.Warning("failed to read node data from local store")
	}

	if nodeData != nil {
		// create node using persisted node data
		return newLocalNodeFromFile(address, nodeData, persist)
	}

	// generate new node
	return NewNodeIdentity(config, address, persist)
}

// NewNodeIdentity creates a new local node without attempting to restore node from local store.
func NewNodeIdentity(config nodeconfig.Config, address string, persist bool) (*LocalNode, error) {
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	return newLocalNodeWithKeys(pub, priv, address, config.NetworkID, persist)
}

func newLocalNodeWithKeys(pubKey crypto.PublicKey, privKey crypto.PrivateKey, address string, networkID int, persist bool) (*LocalNode, error) {

	n := &LocalNode{
		Node: Node{
			pubKey:  pubKey,
			address: address,
		},
		networkID: nodeconfig.NetworkID(networkID),
		privKey:   privKey,
	}

	dataDir, err := filesystem.EnsureNodesDataDirectory(nodeconfig.NodesDirectoryName)
	if err != nil {
		return nil, err
	}

	nodeDir, err := filesystem.EnsureNodeDataDirectory(dataDir, n.pubKey.String())
	if err != nil {
		return nil, err
	}

	// setup logging
	n.Log = log.New(n.pubKey.Pretty(), nodeDir, "node.log")

	n.Info("Local node identity >> %v", n.String())

	ok := n.RefreshPubTCPAddress()
	if !ok {
		return nil, errors.New("critical error - failed to obtain node public ip address. Check your Internet connection and try again")
	}

	n.Debug("Node public ip address %s. Please make sure that your home router or access point accepts incoming connections on this port and forwards incoming such connection requests to this computer.", n.pubTCPAddress)

	if persist {
		// persist store data so we can start it on future app sessions
		err = n.persistData()
		if err != nil { // no much use of starting if we can't store node private key in store
			n.Error("failed to persist node data to local store", err)
			return nil, err
		}
	}

	return n, nil
}

// Creates a new node from persisted NodeData.
func newLocalNodeFromFile(address string, d *nodeFileData, persist bool) (*LocalNode, error) {

	priv, err := crypto.NewPrivateKeyFromString(d.PrivKey)
	if err != nil {
		return nil, err
	}

	pub, err := crypto.NewPublicKeyFromString(d.PubKey)
	if err != nil {
		return nil, err
	}

	log.Info(">>>> Creating node identity from filesystem existing key %s", pub.String())

	return newLocalNodeWithKeys(pub, priv, address, d.NetworkID, persist)
}
