package node

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"strings"
)

// Node is the basic node identity struct
type Node struct {
	pubKey  p2pcrypto.PublicKey
	address string
}

// EmptyNode represents an uninitialized node
var EmptyNode Node

// PublicKey returns the public key of the node
func (n Node) PublicKey() p2pcrypto.PublicKey {
	return n.pubKey
}

// String returns a string representation of the node's public key
func (n Node) String() string {
	return n.pubKey.String()
}

// Address returns the tcp ip:port address of the node
func (n Node) Address() string {
	return n.address
}

// DhtID creates a dhtid from the public key
func (n Node) DhtID() DhtID {
	return NewDhtID(n.pubKey.Bytes())
}

// Pretty returns a pretty string from the node's info
func (n Node) Pretty() string {
	return fmt.Sprintf("Node : %v , Address: %v, DhtID: %v", n.pubKey.String(), n.address, n.DhtID().Pretty())
}

// New creates a new remotenode identity from a public key and an address
func New(key p2pcrypto.PublicKey, address string) Node {
	return Node{key, address}
}

// NewNodeFromString creates a remote identity from a string in the following format: 126.0.0.1:3572/r9gJRWVB9JVPap2HKnduoFySvHtVTfJdQ4WG8DriUD82 .
func NewNodeFromString(data string) (Node, error) {
	items := strings.Split(data, "/")
	if len(items) != 2 {
		return EmptyNode, fmt.Errorf("could'nt create node from string, wrong format")
	}
	pubk, err := p2pcrypto.NewPublicKeyFromBase58(items[1])
	if err != nil {
		return EmptyNode, err
	}
	return Node{pubk, items[0]}, nil
}

// StringFromNode generates a string that represent a node in the network in following format: 126.0.0.1:3572/r9gJRWVB9JVPap2HKnduoFySvHtVTfJdQ4WG8DriUD82.
func StringFromNode(n Node) string {
	return strings.Join([]string{n.Address(), n.PublicKey().String()}, "/")
}
