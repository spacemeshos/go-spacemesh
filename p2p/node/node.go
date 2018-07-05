package node

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"strings"
)

// Node is the basic node identity struct
type Node struct {
	pubKey  crypto.PublicKey
	address string
}

// EmptyNode represents an uninitialized node
var EmptyNode Node

// PublicKey returns the public key of the node
func (n Node) PublicKey() crypto.PublicKey {
	return n.pubKey
}

// String returns a string representation of the node's public key
func (n Node) String() string {
	return n.pubKey.String()
}

// Address returns the ip address of the node
func (n Node) Address() string {
	return n.address
}

// DhtID creates a dhtid from the public key
func (n Node) DhtID() DhtID {
	return NewDhtID(n.pubKey.Bytes())
}

// Pretty returns a pretty string from the node's info
func (n Node) Pretty() string {
	return fmt.Sprintf("Node : %v , Address: %v, DhtID: %v", n.pubKey.Pretty(), n.address, n.DhtID().Pretty())
}

// ToNodeInfo returns marshaled protobufs identity infos slice from a slice of RemoteNodeData.
// filterId: identity id to exclude from the result
func ToNodeInfo(nodes []Node, filterID string) []*pb.NodeInfo {
	// init empty slice
	res := []*pb.NodeInfo{}
	for _, n := range nodes {

		if n.String() == filterID {
			continue
		}

		res = append(res, &pb.NodeInfo{
			NodeId:  n.pubKey.Bytes(),
			Address: n.address,
		})
	}
	return res
}

// Union returns a union of 2 lists of nodes.
func Union(list1 []Node, list2 []Node) []Node {

	idSet := map[string]Node{}

	for _, n := range list1 {
		idSet[n.String()] = n
	}
	for _, n := range list2 {
		if _, ok := idSet[n.String()]; !ok {
			idSet[n.String()] = n
		}
	}

	res := make([]Node, len(idSet))
	i := 0
	for _, n := range idSet {
		res[i] = n
		i++
	}

	return res
}

// SortByDhtID Sorts a Node array by DhtID id, returns a sorted array
func SortByDhtID(nodes []Node, id DhtID) []Node {
	for i := 1; i < len(nodes); i++ {
		v := nodes[i]
		j := i - 1
		for j >= 0 && id.Closer(v.DhtID(), nodes[j].DhtID()) {
			nodes[j+1] = nodes[j]
			j = j - 1
		}
		nodes[j+1] = v
	}
	return nodes
}

// FromNodeInfos converts a list of NodeInfo to a list of Node.
func FromNodeInfos(nodes []*pb.NodeInfo) []Node {
	res := make([]Node, len(nodes))
	for i, n := range nodes {
		pubk, err := crypto.NewPublicKey(n.NodeId)
		if err != nil {
			// TODO Error handling
			continue
		}
		node := Node{pubk, n.Address}
		res[i] = node

	}
	return res
}

// New creates a new remotenode identity from a public key and an address
func New(key crypto.PublicKey, address string) Node {
	return Node{key, address}
}

// NewNodeFromString creates a remote identity from a string in the following format: 126.0.0.1:3572/QmcjTLy94HGFo4JoYibudGeBV2DSBb6E4apBjFsBGnMsWa .
func NewNodeFromString(data string) (Node, error) {
	items := strings.Split(data, "/")
	if len(items) != 2 {
		return EmptyNode, fmt.Errorf("could'nt create node from string, wrong format")
	}
	pubk, err := crypto.NewPublicKeyFromString(items[1])
	if err != nil {
		return EmptyNode, err
	}
	return Node{pubk, items[0]}, nil
}
