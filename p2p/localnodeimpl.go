package p2p

import (
	"encoding/hex"
	"fmt"
	"time"

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

// LocalNode implementation.
type localNodeImp struct {
	pubKey        crypto.PublicKey
	privKey       crypto.PrivateKey
	tcpAddress    string
	pubTCPAddress string
	dhtID         dht.ID

	logger *logging.Logger
	config nodeconfig.Config

	swarm Swarm // local owns a swarm
	ping  Ping

	shutdownChan chan bool
	// add all other implemented protocols here....

}

// NewProtocolMessageMetadata creates meta-data for an outgoing protocol message authored by this node.
func (n *localNodeImp) NewProtocolMessageMetadata(protocol string, reqID []byte, gossip bool) *pb.Metadata {
	return &pb.Metadata{
		Protocol:      protocol,
		ReqId:         reqID,
		ClientVersion: nodeconfig.ClientVersion,
		Timestamp:     time.Now().Unix(),
		Gossip:        gossip,
		AuthPubKey:    n.PublicKey().Bytes(),
	}
}

// GetRemoteNodeData returns the RemoteNodeData for this local node.
func (n *localNodeImp) GetRemoteNodeData() node.RemoteNodeData {
	return node.NewRemoteNodeData(n.String(), n.TCPAddress())
}

// Config returns the local node config params.
func (n *localNodeImp) Config() nodeconfig.Config {
	return n.config
}

// NotifyOnChannel gives the node a callback channel to notify when it shuts down.
func (n *localNodeImp) NotifyOnShutdown(notify chan bool) {
	n.shutdownChan = notify
}

// Shutdown releases all resources open and owned by this local node.
func (n *localNodeImp) Shutdown() {
	// shutdown swarm
	n.swarm.Shutdown()
	if n.shutdownChan != nil {
		n.shutdownChan <- true
	}
}

// GetPing returns this node's Ping protocol.
func (n *localNodeImp) GetPing() Ping {
	return n.ping
}

// Swarm returns this node's swarm.
func (n *localNodeImp) GetSwarm() Swarm {
	return n.swarm
}

// ID() returns this node's ID.
func (n *localNodeImp) ID() []byte {
	return n.pubKey.Bytes()
}

// DhtID() returns this node's dht ID.
func (n *localNodeImp) DhtID() dht.ID {
	return n.dhtID
}

// String() returns a string identifier for this node.
func (n *localNodeImp) String() string {
	return n.pubKey.String()
}

// Pretty returns a readable short identifier for this node.
func (n *localNodeImp) Pretty() string {
	return n.pubKey.Pretty()
}

// TCPAddress returns the TCP address that this node is listening on for incoming network connections.
func (n *localNodeImp) TCPAddress() string {
	return n.tcpAddress
}

// PubTCPAddress returns the node's public tcp address.
func (n *localNodeImp) PubTCPAddress() string {
	addr := n.pubTCPAddress
	return addr
}

// RefreshPubTCPAddress attempts to refresh the node public ip address and returns true if it was able to do so.
func (n *localNodeImp) RefreshPubTCPAddress() bool {

	// Figure out node public ip address
	addr, err := GetPublicIPAddress()
	if err != nil {
		log.Error("failed to obtain public ip address")
		return false
	}

	port, err := GetPort(n.tcpAddress)
	if err != nil {
		log.Error("Invalid tcp ip address", err)
		return false
	}

	n.pubTCPAddress = fmt.Sprintf("%s:%s", addr, port)
	return true
}

// PrivateKey returns this node's private key.
func (n *localNodeImp) PrivateKey() crypto.PrivateKey {
	return n.privKey
}

// PublicKey returns this node's public key.
func (n *localNodeImp) PublicKey() crypto.PublicKey {
	return n.pubKey
}

// SignToString signs a protobufs message with this node's private key, and returns a hex-encoded string signature.
func (n *localNodeImp) SignToString(data proto.Message) (string, error) {
	sign, err := n.Sign(data)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sign), nil
}

// Sign signs a protobufs message with this node's private key and returns the raw signature bytes.
func (n *localNodeImp) Sign(data proto.Message) ([]byte, error) {
	bin, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}

	sign, err := n.PrivateKey().Sign(bin)
	if err != nil {
		return nil, err
	}

	return sign, nil
}

// log wrappers - log node id and args

func (n *localNodeImp) GetLogger() *logging.Logger {
	return n.logger
}

// Info is used for info logging.
func (n *localNodeImp) Info(format string, args ...interface{}) {
	n.logger.Info(format, args...)
}

// Debug is used to log debug data.
func (n *localNodeImp) Debug(format string, args ...interface{}) {
	n.logger.Debug(format, args...)
}

// Error is used to log runtime errors.
func (n *localNodeImp) Error(format string, args ...interface{}) {
	n.logger.Error(format, args...)
}

// Warning is used to log runtime warnings.
func (n *localNodeImp) Warning(format string, args ...interface{}) {
	n.logger.Warning(format, args...)
}

// CreateAccount creates account.
func (n *localNodeImp) CreateAccount(generatePassphrase bool, accountInfo string) error {
	return nil
}

// LocalAccount returns local account.
func (n *localNodeImp) LocalAccount() *accounts.Account {
	acct, err := accounts.NewAccount("")
	if err != nil {
		panic(err)
	}

	return acct
}

// Unlock unlock local account or the account by passphrase.
func (n *localNodeImp) Unlock(passphrase string) error {
	return nil
}

// IsAccountUnLock checks if the account with id is unlock.
func (n *localNodeImp) IsAccountUnLock(id string) bool {
	return false
}

// Unlock Lock local account or the account by passphrase.
func (n *localNodeImp) Lock(passphrase string) error {
	return nil
}

// AccountInfo prints account info.
func (n *localNodeImp) AccountInfo(id string) {
}

// Transfer transfers the amount from an account to the other.
func (n *localNodeImp) Transfer(from, to, amount, passphrase string) error {
	return nil
}

// SetVariables sets params or CLI flags values.
func (n *localNodeImp) SetVariables(params, flags []string) error {
	return nil
}

// NeedRestartNode checks if the params and flags that will be set need
// restart the node.
func (n *localNodeImp) NeedRestartNode(params, flags []string) bool {
	return false
}

// Restart restarts node.
func (n *localNodeImp) Restart(params, flags []string) error {
	return nil
}

// Setup setup POST.
func (n *localNodeImp) Setup(allocation string) error {
	return nil
}
