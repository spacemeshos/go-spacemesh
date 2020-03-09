package node

import (
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

// LocalNode implementation.
type LocalNode struct {
	publicKey p2pcrypto.PublicKey
	privKey   p2pcrypto.PrivateKey
}

func (n LocalNode) PublicKey() p2pcrypto.PublicKey {
	return n.publicKey
}

// PrivateKey returns this node's private key.
func (n LocalNode) PrivateKey() p2pcrypto.PrivateKey {
	return n.privKey
}

var emptyNode LocalNode

// NewNodeIdentity creates a new local node without attempting to restore node from local store.
func NewNodeIdentity() (LocalNode, error) {
	priv, pub, err := p2pcrypto.GenerateKeyPair()
	if err != nil {
		return emptyNode, err
	}
	return LocalNode{
		publicKey: pub,
		privKey:   priv,
	}, nil
}

// Creates a new node from persisted NodeData.
func newLocalNodeFromFile(d *nodeFileData) (LocalNode, error) {

	priv, err := p2pcrypto.NewPrivateKeyFromBase58(d.PrivKey)
	if err != nil {
		return emptyNode, err
	}

	pub, err := p2pcrypto.NewPublicKeyFromBase58(d.PubKey)
	if err != nil {
		return emptyNode, err
	}

	n := LocalNode{
		publicKey: pub,
		privKey:   priv,
	}

	return n, nil
}
