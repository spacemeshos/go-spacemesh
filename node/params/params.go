package params

import (
	ma "gx/ipfs/QmXY77cVe7rVRQXZZQRioukUM7aRW3BTcAgJe12MCtb3Ji/go-multiaddr"
	"log"
)

// params are non-configurable (hard-coded) consts. To create a configurable param use Config

type NodeInfo struct {
	Id string	// base58 node id
	Address ma.Multiaddr
}

func NewNodeInfo(id string, address string) (*NodeInfo) {
	maddr, err := ma.NewMultiaddr(address)
	if err != nil {
		log.Println(err, "Failed to create multi-address from provided address string")
		return nil
	}
	return &NodeInfo{Id:id, Address:maddr}
}

var (
	BootstrapNodes = [] *NodeInfo {
		NewNodeInfo("foo", "/ip4/127.0.0.1/tcp/3571"),
		NewNodeInfo("bar", "/ip4/126.0.0.1/tcp/3571"),
		NewNodeInfo("goo", "/ip4/125.0.0.1/tcp/3571"),
	}
)