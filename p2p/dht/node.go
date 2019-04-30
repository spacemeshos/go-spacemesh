package dht

import (
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"net"
)

type discNode struct {
	node.Node
	udpAddress string
	parsedIP   net.IP
}

func discNodeFromNode(nd node.Node, udpAddress string) discNode {
	raw := nd.Address()
	ip, _, err := net.SplitHostPort(raw)
	if err != nil {
		return emptyDiscNode
	}
	return discNode{nd, udpAddress, net.ParseIP(ip)}
}

var emptyDiscNode = discNode{node.EmptyNode, "", nil}
