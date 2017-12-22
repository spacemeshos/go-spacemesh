package p2p

import (
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
