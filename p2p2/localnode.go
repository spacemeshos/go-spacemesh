package p2p2

// The local unruly node is the root of all evil
type LocalNode interface {
	Id() []byte
	String() string
	Pretty() string

	PrivateKey() PrivateKey
	PublicKey() PublicKey
}

func NewLocalNode(pubKey PublicKey, privKey PrivateKey, tcpAddress string) LocalNode {
	return &localNodeImp{pubKey,
		privKey,
		tcpAddress}
}

// Node implementation type
type localNodeImp struct {
	pubKey     PublicKey
	privKey    PrivateKey
	tcpAddress string
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
