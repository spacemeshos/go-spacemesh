package discovery

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"net"
	"time"
)

type protocolRoutingTable interface {
	GetAddress() *KnownAddress
	AddAddresses(n []*node.NodeInfo, src *node.NodeInfo)
	AddAddress(n *node.NodeInfo, src *node.NodeInfo)
	AddressCache() []*node.NodeInfo
}

type protocol struct {
	local     *node.NodeInfo
	table     protocolRoutingTable
	logger    log.Log
	msgServer *server.MessageServer
}

func (d *protocol) SetLocalAddresses(tcp, udp int) {
	d.local.ProtocolPort = uint16(tcp)
	d.local.DiscoveryPort = uint16(udp)
}

// Name is the name if the protocol.
const Name = "/udp/v2/discovery"

// MessageBufSize is the buf size we give to the messages channel
const MessageBufSize = 1000

// MessageTimeout is the timeout we tolerate when waiting for a message reply
const MessageTimeout = time.Second * 5 // TODO: Parametrize

// PINGPONG is the ping protocol ID
const PINGPONG = 0

// GET_ADDRESSES is the findnode protocol ID
const GET_ADDRESSES = 1

// NewDiscoveryProtocol is a constructor for a protocol protocol provider.
func NewDiscoveryProtocol(local p2pcrypto.PublicKey, rt protocolRoutingTable, svc server.Service, log log.Log) *protocol {
	s := server.NewMsgServer(svc, Name, MessageTimeout, make(chan service.DirectMessage, MessageBufSize), log)
	d := &protocol{
		local:     &node.NodeInfo{ID: local.Array(), IP: net.IPv4zero, ProtocolPort: 7513, DiscoveryPort: 7513},
		table:     rt,
		msgServer: s,
		logger:    log,
	}

	// XXX Reminder: for discovery protocol to work you must call SetLocalAddresses with updated ports from the socket.

	d.msgServer.RegisterMsgHandler(PINGPONG, d.newPingRequestHandler())
	d.msgServer.RegisterMsgHandler(GET_ADDRESSES, d.newGetAddressesRequestHandler())
	return d
}

func (p *protocol) Close() {
	p.msgServer.Close()
}
