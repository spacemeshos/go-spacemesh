package swarm

import (
	"fmt"
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/p2p2/keys"
	"testing"
)

func generateTestNode(t *testing.T) (LocalNode, RemoteNode) {

	port := crypto.GetRandomUInt32(1000) + 10000
	address := fmt.Sprintf("localhost:%d", port)

	priv, pub, _ := keys.GenerateKeyPair()
	localNode, err := NewLocalNode(pub, priv, address)

	if err != nil {
		t.Error("failed to create local node1", err)
	}

	// this will be node 2 view of node 1
	remoteNode, err := NewRemoteNode(pub.String(), address)

	if err != nil {
		t.Error("failed to create remote node1", err)
	}

	return localNode, remoteNode
}

// Basic handshake protocol data test
func TestSessionCreation(t *testing.T) {

	callback := make(chan HandshakeData)

	node1Local, _ := generateTestNode(t)
	_, node2Remote := generateTestNode(t)

	node1Local.GetSwarm().getHandshakeProtocol().RegisterNewSessionCallback(callback)
	node1Local.GetSwarm().ConnectTo(RemoteNodeData{node2Remote.String(), node2Remote.TcpAddress()})

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
