package swarm

import (
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p2/keys"
)


// The local unruly node is the root of all evil
type LocalNode interface {
	Id() []byte
	String() string
	Pretty() string

	PrivateKey() keys.PrivateKey
	PublicKey() keys.PublicKey

	TcpAddress() string
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
