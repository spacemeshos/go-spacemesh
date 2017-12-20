package p2p

import (
	"github.com/UnrulyOS/go-unruly/p2p/node"
	"testing"
)

// Basic handshake protocol data test
func TestSessionCreation(t *testing.T) {

	callback := make(chan HandshakeData)

	node1Local, _ := GenerateTestNode(t)
	_, node2Remote := GenerateTestNode(t)

	node1Local.GetSwarm().getHandshakeProtocol().RegisterNewSessionCallback(callback)
	node1Local.GetSwarm().ConnectTo(node.NewRemoteNodeData(node2Remote.String(), node2Remote.TcpAddress()))

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
