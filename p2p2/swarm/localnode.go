package swarm

import (
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p2/keys"
	"github.com/UnrulyOS/go-unruly/p2p2/swarm/pb"
	"github.com/golang/protobuf/proto"
	"time"
)

const clientVersion = "og-unruly-0.0.1"

// The local unruly node is the root of all evil
type LocalNode interface {
	Id() []byte
	String() string
	Pretty() string

	PrivateKey() keys.PrivateKey
	PublicKey() keys.PublicKey

	TcpAddress() string

	NewProtocolMessageMetadata(protocol string, reqId []byte, gossip bool) *pb.Metadata

	SignMessage(data proto.Message) ([]byte, error)

	GetSwarm() Swarm

	GetPing() Ping
}

func NewLocalNode(pubKey keys.PublicKey, privKey keys.PrivateKey, tcpAddress string) (LocalNode, error) {

	n := &localNodeImp{
		pubKey:     pubKey,
		privKey:    privKey,
		tcpAddress: tcpAddress,
	}

	s, err := NewSwarm(tcpAddress, n)
	if err != nil {
		log.Error("can't create a local node without a swarm. %v", err)
		return nil, err
	}

	n.swarm = s
	n.ping = NewPingProtocol(s)

	// create all implemented local node protocols here

	return n, nil
}

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

// Sign a protobufs message
func (n *localNodeImp) SignMessage(data proto.Message) ([]byte, error) {
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
