package swarm

import (
	"encoding/hex"
	"github.com/UnrulyOS/go-unruly/p2p2/keys"
	"github.com/UnrulyOS/go-unruly/p2p2/swarm/pb"
	"github.com/golang/protobuf/proto"
	"time"
)

// Node implementation type
type localNodeImp struct {
	pubKey     keys.PublicKey
	privKey    keys.PrivateKey
	tcpAddress string

	// local owns a swarm
	swarm Swarm

	// add all other protocols here
	ping Ping
}

// Create meta-data for an outgoing protocol message authored by this node
func (n *localNodeImp) NewProtocolMessageMetadata(protocol string, reqId []byte, gossip bool) *pb.Metadata {
	return &pb.Metadata{
		Protocol:      protocol,
		ReqId:         reqId,
		ClientVersion: clientVersion,
		Timestamp:     time.Now().Unix(),
		Gossip:        gossip,
		AuthPubKey:    n.PublicKey().Bytes(),
	}
}

func (n *localNodeImp) SendMessage(req SendMessageReq) {
	n.swarm.SendMessage(req)
}

func (n *localNodeImp) Shutdown() {
	// todo - kill swarm
}

func (n *localNodeImp) GetPing() Ping {
	return n.ping
}

func (n *localNodeImp) GetSwarm() Swarm {
	return n.swarm
}

func (n *localNodeImp) TcpAddress() string {
	return n.tcpAddress
}

func (n *localNodeImp) Id() []byte {
	return n.pubKey.Bytes()
}

func (n *localNodeImp) String() string {
	return n.pubKey.String()
}

func (n *localNodeImp) Pretty() string {
	return n.pubKey.Pretty()
}

func (n *localNodeImp) PrivateKey() keys.PrivateKey {
	return n.privKey
}

func (n *localNodeImp) PublicKey() keys.PublicKey {
	return n.pubKey
}

func (n *localNodeImp) SignToString(data proto.Message) (string, error) {
	sign, err := n.Sign(data)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sign), nil
}

// Sign a protobufs message
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
