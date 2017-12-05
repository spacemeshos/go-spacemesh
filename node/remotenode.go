package node

import (
	"github.com/UnrulyOS/go-unruly/log"
	ma "gx/ipfs/QmXY77cVe7rVRQXZZQRioukUM7aRW3BTcAgJe12MCtb3Ji/go-multiaddr"
	"strings"
)

// Barebones remote node data. Used for bootstrap node
type RemoteNodeData struct {
	Id        string         // base58 encoded node id str
	Addresses []ma.Multiaddr // e.g. ['/ip4/127.0.0.1/tcp/3571']
}

// Create a node info from a node multi-address url
// url: multi-address url. e.g. /ip4/127.0.0.1/tcp/3571/node1id where no id is a base 58 string
func newRemoteNodeData(murl string) *RemoteNodeData {

	idx := strings.LastIndex(murl, "/")
	if idx == -1 {
		log.Error("Error parsing node url %s", murl)
		return nil
	}
	address := murl[0:idx]
	nodeID := murl[idx+1:]
	maddr, err := ma.NewMultiaddr(address)

	if err != nil {
		log.Error("Failed to create multi-address from provided murl string. %s.", err)
		return nil
	}

	return &RemoteNodeData{Id: nodeID, Addresses: []ma.Multiaddr{maddr}}
}

// Create remote node data from a set of murls
func RemoteNodesFromMurls(murls []string) []RemoteNodeData {

	nodes := make([]RemoteNodeData, 0, 3)
	for _, murl := range murls {
		node := newRemoteNodeData(murl)
		if node != nil {
			nodes = append(nodes, *node)
		}
	}
	return nodes
}
