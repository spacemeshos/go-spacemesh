package swarm

import (
	"bytes"
	"fmt"
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p2/keys"
	"github.com/UnrulyOS/go-unruly/p2p2/swarm/pb"
	"github.com/google/uuid"
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

func TestPingProtocol(t *testing.T) {

	node1Local, _ := generateTestNode(t)
	_, node2Remote := generateTestNode(t)

	// tell node 1 about node 2
	node1Local.GetSwarm().RegisterNode(RemoteNodeData{node2Remote.String(), node2Remote.TcpAddress()})

	// 4 lines and a callback on a channel
	pingReqId := []byte (uuid.New().String())
	callback := make(chan * pb.PingRespData)
	node1Local.GetPing().RegisterCallback(callback)
	node1Local.GetPing().SendPing("hello unruly", pingReqId, node2Remote.String())

Loop:
	for {
		select {
		case c := <-callback:
			if bytes.Equal(c.GetMetadata().ReqId, pingReqId) {
				log.Info("Got pong: `%s`", c.GetPong())
				break Loop
			}
		}
	}
}



