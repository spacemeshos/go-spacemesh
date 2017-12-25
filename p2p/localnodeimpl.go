package p2p

import (
	"encoding/hex"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"time"
)

// Node implementation type
type localNodeImp struct {
	pubKey     crypto.PublicKey
	privKey    crypto.PrivateKey
	tcpAddress string
	dhtId      dht.ID

	config nodeconfig.Config

	swarm Swarm // local owns a swarm

	ping Ping

	// add all other implemented protocols here....

}

// Create meta-data for an outgoing protocol message authored by this node
func (n *localNodeImp) NewProtocolMessageMetadata(protocol string, reqId []byte, gossip bool) *pb.Metadata {
	return &pb.Metadata{
		Protocol:      protocol,
		ReqId:         reqId,
		ClientVersion: nodeconfig.ClientVersion,
		Timestamp:     time.Now().Unix(),
		Gossip:        gossip,
		AuthPubKey:    n.PublicKey().Bytes(),
	}
}

func (n *localNodeImp) SendMessage(req SendMessageReq) {
	// let the swarm handle
	//n.swarm.SendMessage(req)
}

// helper - return remote node data of this node
func (n *localNodeImp) GetRemoteNodeData() node.RemoteNodeData {
	return node.NewRemoteNodeData(n.String(), n.TcpAddress())
}

func (n *localNodeImp) Config() nodeconfig.Config {
	return n.config
}

func (n *localNodeImp) Shutdown() {
	// todo - kill swarm and stop all services
}

func (n *localNodeImp) GetPing() Ping {
	return n.ping
}

func (n *localNodeImp) GetSwarm() Swarm {
	return n.swarm
}

func (n *localNodeImp) Id() []byte {
	return n.pubKey.Bytes()
}

func (n *localNodeImp) DhtId() dht.ID {
	return n.dhtId
}

func (n *localNodeImp) String() string {
	return n.pubKey.String()
}

func (n *localNodeImp) Pretty() string {
	return n.pubKey.Pretty()
}

func (n *localNodeImp) TcpAddress() string {
	return n.tcpAddress
}

func (n *localNodeImp) PrivateKey() crypto.PrivateKey {
	return n.privKey
}

func (n *localNodeImp) PublicKey() crypto.PublicKey {
	return n.pubKey
}

// Sign a protobufs message and return a hex-encoded string signature
func (n *localNodeImp) SignToString(data proto.Message) (string, error) {
	sign, err := n.Sign(data)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sign), nil
}

// Sign a protobufs message and return raw signature bytes
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
