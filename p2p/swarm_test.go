package p2p

import (
	"bytes"
	"fmt"
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p/pb"
	"github.com/google/uuid"
	"testing"
)

func generateTestNode(t *testing.T) (LocalNode, RemoteNode) {

	port := crypto.GetRandomUInt32(1000) + 10000
	address := fmt.Sprintf("localhost:%d", port)

	localNode, err := NewLocalNode(address)
	if err != nil {
		t.Error("failed to create local node1", err)
	}

	// this will be node 2 view of node 1
	remoteNode, err := NewRemoteNode(localNode.String(), address)
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

	// let node 1 know about node 2
	node1Local.GetSwarm().RegisterNode(RemoteNodeData{node2Remote.String(), node2Remote.TcpAddress()})

	// 4 lines of code and a callback on a channel !
	pingReqId := []byte(uuid.New().String())
	callback := make(chan *pb.PingRespData)
	node1Local.GetPing().Register(callback)
	node1Local.GetPing().Send("hello unruly", pingReqId, node2Remote.String())

	// internally, node 1 creates an encrypted authenticated session with node 2 and sends the ping request
	// over that session once it is established. Node 1 registers an app-level callback to get the ping response from node 2.
	// The response includes the request id so it can match it with one or more tracked requests it sent.

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
