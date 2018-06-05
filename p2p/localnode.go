package p2p

import (
	"errors"

	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/accounts"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"gopkg.in/op/go-logging.v1"
)

// LocalNode specifies local Spacemesh node capabilities and services.
type LocalNode interface {
	ID() []byte
	String() string
	Pretty() string

	PrivateKey() crypto.PrivateKey
	PublicKey() crypto.PublicKey

	DhtID() dht.ID
	TCPAddress() string    // ipv4 tcp address that the node is listing on
	PubTCPAddress() string // node's public ip address - advertised by this node

	RefreshPubTCPAddress() bool // attempt to refresh the node's public ip address

	Sign(data proto.Message) ([]byte, error)
	SignToString(data proto.Message) (string, error)
	NewProtocolMessageMetadata(protocol string, reqID []byte, gossip bool) *pb.Metadata

	GetSwarm() Swarm
	GetPing() Ping

	Config() nodeconfig.Config

	GetRemoteNodeData() node.RemoteNodeData

	Shutdown()

	NotifyOnShutdown(chan bool)

	// logging wrappers - log node id and args
	GetLogger() *logging.Logger

	Info(format string, args ...interface{})
	Debug(format string, args ...interface{})
	Error(format string, args ...interface{})
	Warning(format string, args ...interface{})

	// local store persistence
	EnsureNodeDataDirectory() (string, error)
	persistData() error

	CreateAccount(generatePassphrase bool, accountInfo string) error
	LocalAccount() *accounts.Account
	Unlock(passphrase string) error
	IsAccountUnLock(id string) bool
	Lock(passphrase string) error
	AccountInfo(id string)
	Transfer(from, to, amount, passphrase string) error
	SetVariables(params, flags []string) error
	Restart(params, flags []string) error
	NeedRestartNode(params, flags []string) bool
	Setup(allocation string) error
}

// NewLocalNode creates a local node with a provided tcp address.
// Attempts to set node identity from persisted data in local store.
// Creates a new identity if none was loaded.
func NewLocalNode(tcpAddress string, config nodeconfig.Config, persist bool) (LocalNode, error) {

	if len(nodeconfig.ConfigValues.NodeID) > 0 {
		// user provided node id/pubkey via the cli - attempt to start that node w persisted data
		data, err := readNodeData(nodeconfig.ConfigValues.NodeID)
		if err != nil {
			return nil, err
		}

		return newNodeFromData(tcpAddress, data, config, persist)
	}

	// look for persisted node data in the nodes directory
	// load the node with the data of the first node found
	nodeData, err := readFirstNodeData()
	if err != nil {
		log.Warning("failed to read node data from local store")
	}

	if nodeData != nil {
		// crete node using persisted node data
		return newNodeFromData(tcpAddress, nodeData, config, persist)
	}

	// generate new node
	return NewNodeIdentity(tcpAddress, config, persist)
}

// NewNodeIdentity creates a new local node without attempting to restore identity from local store.
func NewNodeIdentity(tcpAddress string, config nodeconfig.Config, persist bool) (LocalNode, error) {
	priv, pub, _ := crypto.GenerateKeyPair()
	return newLocalNodeWithKeys(pub, priv, tcpAddress, config, persist)
}

func newLocalNodeWithKeys(pubKey crypto.PublicKey, privKey crypto.PrivateKey, tcpAddress string,
	config nodeconfig.Config, persist bool) (LocalNode, error) {

	n := &localNodeImp{
		pubKey:     pubKey,
		privKey:    privKey,
		tcpAddress: tcpAddress,
		config:     config, // store this node passed-in config values and use them later
		dhtID:      dht.NewIDFromNodeKey(pubKey.Bytes()),
	}

	dataDir, err := n.EnsureNodeDataDirectory()
	if err != nil {
		return nil, err
	}

	// setup logging
	n.logger = log.CreateLogger(n.pubKey.Pretty(), dataDir, "node.log")

	n.Debug("Node id: %s", n.String())

	// swarm owned by node
	s, err := NewSwarm(tcpAddress, n)
	if err != nil {
		n.Error("can't create a local node without a swarm", err)
		return nil, err
	}

	ok := n.RefreshPubTCPAddress()
	if !ok {
		return nil, errors.New("critical error - failed to obtain node public ip address. Check your Internet connection and try again")
	}

	n.Debug("Node public ip address %s. Please make sure that your home router or access point accepts incoming connections on this port and forwards incoming such connection requests to this computer.", n.pubTCPAddress)

	n.swarm = s
	n.ping = NewPingProtocol(s)

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
func newNodeFromData(tcpAddress string, d *NodeData, config nodeconfig.Config, persist bool) (LocalNode, error) {

	priv, err := crypto.NewPrivateKeyFromString(d.PrivKey)
	if err != nil {
		return nil, err
	}

	pub, err := crypto.NewPublicKeyFromString(d.PubKey)
	if err != nil {
		return nil, err
	}

	log.Debug(">>>> Creating node from existing key %s", pub.String())

	return newLocalNodeWithKeys(pub, priv, tcpAddress, config, persist)
}
