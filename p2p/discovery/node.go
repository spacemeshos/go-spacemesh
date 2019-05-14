package discovery

import (
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"net"
)

// NodeInfo is a discovery parsed structure to store a node's address and key.
type NodeInfo struct {
	node.Node
	udpAddress string
	parsedIP   net.IP
}

func NodeInfoFromNode(nd node.Node, udpAddress string) NodeInfo {
	raw := nd.Address()
	ip, _, err := net.SplitHostPort(raw)
	if err != nil {
		return emptyNodeInfo
	}
	return NodeInfo{nd, udpAddress, net.ParseIP(ip)}
}

var emptyNodeInfo = NodeInfo{node.EmptyNode, "", nil}
