package p2p2

type LocalNode interface {
	Id() []byte
	String() string
	Pretty() string

	PrivateKey() PrivateKey
	PublicKey() PublicKey
}

type localNodeImp struct {
	pubKey     PublicKey
	privKey    PrivateKey
	tcpAddress string
}

func NewLocalNode(pubKey PublicKey, privKey PrivateKey, tcpAddress string) LocalNode {
	return &localNodeImp{pubKey,
		privKey,
		tcpAddress}
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
