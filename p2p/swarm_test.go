package p2p

import (
	"bytes"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"testing"
	"time"
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
		case <-time.After(time.Second * 10):
			t.Fatalf("Timeout error - failed to create session")
		}
	}
}

func _estSimpleBootstrap(t *testing.T) {

	// setup:
	// node1 - bootstrap node
	// node2 - connects to node1 on startup (bootstrap)
	// node2 - sends a ping to node1

	node1Local, node1Remote := GenerateTestNode(t)

	pd := node1Local.GetRemoteNodeData()
	bs := fmt.Sprintf("%s/%s", pd.Ip(), pd.Id())

	// node1 is a bootstrap node to node2
	c := nodeconfig.ConfigValues
	c.SwarmConfig.Bootstrap = true
	c.SwarmConfig.RandomConnections = 2
	c.SwarmConfig.BootstrapNodes = []string{bs}

	node2Local, _ := GenerateTestNodeWithConfig(t, c)

	// ping node2 -> node 1
	reqId := crypto.UUID()
	callback := make(chan SendPingResp)
	node2Local.GetPing().Register(callback)
	node2Local.GetPing().Send("hello Spacemesh!", reqId, node1Remote.String())

Loop:
	for {
		select {
		case c := <-callback:
			assert.Nil(t, c.err, "expected no err in response")
			if bytes.Equal(c.GetMetadata().ReqId, reqId) {
				break Loop
			}
		case <-time.After(time.Second * 30):
			t.Fatal("Timeout error - expected callback")
		}
	}
}

// todo: fix me - this test is broken
func testBootstrap(t *testing.T) {

	// setup:
	// node1 - bootstrap node
	// node2 - connects to node1 on startup (bootstrap)
	// node3 - connects to node1 on startup (bootstrap)
	// node2 - sends a ping to node3 knowing only its id but not dial info

	node1Local, _ := GenerateTestNode(t)

	pd := node1Local.GetRemoteNodeData()
	bs := fmt.Sprintf("%s/%s", pd.Ip(), pd.Id())

	// node1 and node 2 config
	c := nodeconfig.ConfigValues
	c.SwarmConfig.Bootstrap = true
	c.SwarmConfig.RandomConnections = 2
	c.SwarmConfig.BootstrapNodes = []string{bs}

	node2Local, _ := GenerateTestNodeWithConfig(t, c)
	_, node3Remote := GenerateTestNodeWithConfig(t, c)

	// ping node2 -> node 3
	reqId := crypto.UUID()
	callback := make(chan SendPingResp)
	node2Local.GetPing().Register(callback)
	node2Local.GetPing().Send("hello spacemesh", reqId, node3Remote.String())

Loop:
	for {
		select {
		case c := <-callback:
			assert.Nil(t, c.err, "expected no err in response")
			if bytes.Equal(c.GetMetadata().ReqId, reqId) {
				break Loop
			}
		case <-time.After(time.Second * 30):
			t.Fatal("Timeout error - expected callback")
		}
	}
}
