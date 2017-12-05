package config

import (
	"github.com/UnrulyOS/go-unruly/log"
	ma "gx/ipfs/QmXY77cVe7rVRQXZZQRioukUM7aRW3BTcAgJe12MCtb3Ji/go-multiaddr"
	"strings"
)

const (
	// add all node params here (non-configurable consts) - ideally nost node params should be configurable

	ClientVersion      = "go-p2p-node/0.0.1"
	NodesDirectoryName = "nodes"
	NodeDataFileName   = "id.json"
)

var (
	// multi-address format
	BootstrapNodes = []string{
		"/ip4/127.0.0.1/tcp/3571/QmcjTLy94HGFo4JoYibudGeBV2DSBb6E4apBjFsBGnYaNo",
		"/ip4/126.0.0.1/tcp/3572/QmcjTLy94HGFo4JoYibudGeBV2DSBb6E4apBjFsBGnMsWa",
		"/ip4/125.0.0.1/tcp/3763/QmRtrUMB3rfmRZE6yn8yLRvik6a5Pprvc5HnB1HT8MnoPy",
	}
)

// params are non-configurable (hard-coded) consts. To create a configurable param use Config

// barebones node info
type NodeInfo struct {
	Id        string         // base58 node id
	Addresses []ma.Multiaddr // e.g. ['/ip4/127.0.0.1/tcp/3571']
}

// Create a node info from a node multi-address url
// url: multi-address url. e.g. /ip4/127.0.0.1/tcp/3571/node1id where no id is a base 58 string
func NewNodeInfo(murl string) *NodeInfo {

	idx := strings.LastIndex(murl, "/")
	if idx == -1 {
		log.Error("Error parsing node url %s", murl)
		return nil
	}
	address := murl[0 : idx-1]
	nodeID := murl[:idx+1]
	maddr, err := ma.NewMultiaddr(address)
	if err != nil {
		log.Error("Failed to create multi-address from provided murl string. %s.", err)
		return nil
	}

	return &NodeInfo{Id: nodeID, Addresses: []ma.Multiaddr{maddr}}
}

