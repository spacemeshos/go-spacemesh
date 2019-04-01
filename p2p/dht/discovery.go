package dht

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"time"
)

type protocolRoutingTable interface {
	//Lookup(k p2pcrypto.PublicKey) []node.Node // todo: use for bootstrap ?
	InternalLookup(k p2pcrypto.PublicKey) []node.Node
	Update(n node.Node)
}

type discovery struct {
	local     node.Node
	table     protocolRoutingTable
	logger    log.Log
	msgServer *server.MessageServer
}

// Name is the name if the protocol.
const Name = "/udp/v2/discovery"

// MessageBufSize is the buf size we give to the messages channel
const MessageBufSize = 100

// MessageTimeout is the timeout we tolerate when waiting for a message reply
const MessageTimeout = time.Second * 1 // TODO: Parametrize

// PINGPONG is the ping protocol ID
const PINGPONG = 0

// FIND_NODE is the findnode protocol ID
const FIND_NODE = 1

// NewDiscoveryProtocol is a constructor for a discovery protocol provider.
func NewDiscoveryProtocol(local node.Node, rt protocolRoutingTable, svc server.Service, log log.Log) *discovery {
	s := server.NewMsgServer(svc, Name, time.Second, make(chan service.DirectMessage, MessageBufSize), log)
	d := &discovery{
		local:     local,
		table:     rt,
		msgServer: s,
		logger:    log,
	}

	d.msgServer.RegisterMsgHandler(PINGPONG, d.newPingRequestHandler())
	d.msgServer.RegisterMsgHandler(FIND_NODE, d.newFindNodeRequestHandler())
	return d
}
