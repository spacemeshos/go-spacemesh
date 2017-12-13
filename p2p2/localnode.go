package p2p2

import (
	"github.com/UnrulyOS/go-unruly/log"
)

// The local unruly node is the root of all evil
type LocalNode interface {
	Id() []byte
	String() string
	Pretty() string

	PrivateKey() PrivateKey
	PublicKey() PublicKey

	TcpAddress() string
}

func NewLocalNode(pubKey PublicKey, privKey PrivateKey, tcpAddress string) (LocalNode, error) {

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

	// create all implemented local node protocols here

	return n, nil
}

// Node implementation type
type localNodeImp struct {
	pubKey     PublicKey
	privKey    PrivateKey
	tcpAddress string

	// local owns a swarm
	swarm Swarm

	// add all other protocols here
	handshake HandshakeProtocol
}

func (n *localNodeImp) HandshakeProtocol() HandshakeProtocol {
	return n.handshake
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

func (n *localNodeImp) PrivateKey() PrivateKey {
	return n.privKey
}

func (n *localNodeImp) PublicKey() PublicKey {
	return n.pubKey
}
