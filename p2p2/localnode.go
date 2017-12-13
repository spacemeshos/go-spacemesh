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

	// protocols provided by local node
	HandshakeProtocol() HandshakeProtocol
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

	// create all implemented local node protocols

	n.handshake = NewHandshakeProtocol(s)

	return n, nil
}

// Node implementation type
type localNodeImp struct {
	pubKey     PublicKey
	privKey    PrivateKey
	tcpAddress string
	swarm      Swarm

	handshake HandshakeProtocol
}

func (n *localNodeImp) HandshakeProtocol() HandshakeProtocol {
	return n.handshake
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
