package p2p

import (
	"github.com/UnrulyOS/go-unruly/p2p/nodeconfig"
	"testing"
)

// Basic session test
func TestSessionCreation(t *testing.T) {

	callback := make(chan HandshakeData)

	node1Local, _ := GenerateTestNode(t)
	node2Local, _ := GenerateTestNode(t)

	node1Local.GetSwarm().getHandshakeProtocol().RegisterNewSessionCallback(callback)
	node1Local.GetSwarm().ConnectTo(node2Local.GetRemoteNodeData())

Loop:
	for {
		select {
		case c := <-callback:
			if c.Session().IsAuthenticated() {
				break Loop
			}
		}
	}
}

func TestBootstrap(t *testing.T) {

	// this should attempt to connect with bootstrap nodes and 2 random nodes
	c := nodeconfig.ConfigValues
	c.SwarmConfig.Bootstrap = true
	c.SwarmConfig.RandomConnections = 2
	GenerateTestNodeWithConfig(t, c)

	// todo: test bootstrap nodes are in the routing table
}
