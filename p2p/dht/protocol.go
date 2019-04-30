package dht

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"time"
)

type protocolRoutingTable interface {
	GetAddress() *KnownAddress
	AddAddresses(n []discNode, src discNode)
	AddAddress(n discNode, src discNode)
	AddressCache() []discNode
}

type protocol struct {
	local     node.Node
	table     protocolRoutingTable
	logger    log.Log
	msgServer *server.MessageServer

	localTcpAddress string
	localUdpAddress string
}

func (d *protocol) SetLocalAddresses(tcp, udp string) {
	d.localTcpAddress = tcp
	d.localUdpAddress = udp
}

// Name is the name if the protocol.
const Name = "/udp/v2/protocol"

// MessageBufSize is the buf size we give to the messages channel
const MessageBufSize = 100

// MessageTimeout is the timeout we tolerate when waiting for a message reply
const MessageTimeout = time.Second * 1 // TODO: Parametrize

// PINGPONG is the ping protocol ID
const PINGPONG = 0

// GET_ADDRESSES is the findnode protocol ID
const GET_ADDRESSES = 1

// NewDiscoveryProtocol is a constructor for a protocol protocol provider.
func NewDiscoveryProtocol(local node.Node, rt protocolRoutingTable, svc server.Service, log log.Log) *protocol {
	s := server.NewMsgServer(svc, Name, MessageTimeout, make(chan service.DirectMessage, MessageBufSize), log)
	d := &protocol{
		local:     local,
		table:     rt,
		msgServer: s,
		logger:    log,
	}

	d.SetLocalAddresses(local.Address(), local.Address())

	d.msgServer.RegisterMsgHandler(PINGPONG, d.newPingRequestHandler())
	d.msgServer.RegisterMsgHandler(GET_ADDRESSES, d.newGetAddressesRequestHandler())
	return d
}
