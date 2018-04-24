package p2p

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
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

	node1Local.Shutdown()
	node2Local.Shutdown()
}

func _estSimpleBootstrap(t *testing.T) {

	// setup:
	// node1 - bootstrap node
	// node2 - connects to node1 on startup (bootstrap)
	// node2 - sends a ping to node1

	node1Local, node1Remote := GenerateTestNode(t)

	pd := node1Local.GetRemoteNodeData()
	bs := fmt.Sprintf("%s/%s", pd.IP(), pd.ID())

	// node1 is a bootstrap node to node2
	c := nodeconfig.ConfigValues
	c.SwarmConfig.Bootstrap = true
	c.SwarmConfig.RandomConnections = 2
	c.SwarmConfig.BootstrapNodes = []string{bs}

	node2Local, _ := GenerateTestNodeWithConfig(t, c)

	// ping node2 -> node 1
	reqID := crypto.UUID()
	callback := make(chan SendPingResp)
	node2Local.GetPing().Register(callback)
	node2Local.GetPing().Send("hello Spacemesh!", reqID, node1Remote.String())

Loop:
	for {
		select {
		case c := <-callback:
			assert.Nil(t, c.err, "expected no err in response")
			if bytes.Equal(c.GetMetadata().ReqId, reqID) {
				break Loop
			}
		case <-time.After(time.Second * 30):
			t.Fatal("Timeout error - expected callback")
		}
	}

	node1Local.Shutdown()
	node2Local.Shutdown()
}

func _estBootstrap(t *testing.T) {

	// setup:
	//
	// 1 bootstrap node
	//
	// 10 nodes using bootstrap node
	//
	// each node asks for 5 random nodes and connects to them
	// verify each node has an established session with 5 other nodes

	bnode, _ := GenerateTestNode(t)
	pd := bnode.GetRemoteNodeData()
	bs := fmt.Sprintf("%s/%s", pd.IP(), pd.ID())

	// nodes bootstrap config
	c := nodeconfig.ConfigValues
	c.SwarmConfig.Bootstrap = true
	c.SwarmConfig.RandomConnections = 5
	c.SwarmConfig.BootstrapNodes = []string{bs}

	nodes := make([]LocalNode, 0)

	callbacks := make([]chan NodeEvent, 0)

	for i := 0; i < 10; i++ {
		n, _ := GenerateTestNodeWithConfig(t, c)
		nodes = append(nodes, n)

		callback := make(chan NodeEvent)
		callbacks = append(callbacks, callback)
		n.GetSwarm().RegisterNodeEventsCallback(callback)
	}

	// todo: use callback channels to verify that each node has established sessions with 5 remote random peers

	/*
		var sessions uint64 = 0
		for i := 0; i < 10; i++ {
			for {
				select {
					case c := <- callbacks[i] :
						if c.State == SessionEstablished {
							atomic.AddUint64(&sessions, 1)
						}

						s := atomic.LoadUint64(&sessions)
						if s >= 100 {
							return
						}

				}
			}
		}*/

	for _, node := range nodes {
		node.Shutdown()
	}
}

// todo: fix me - this test is broken
func TestBasicBootstrap(t *testing.T) {

	// setup:
	// node1 - bootstrap node
	// node2 - connects to node1 on startup (bootstrap)
	// node3 - connects to node1 on startup (bootstrap)
	// node2 - sends a ping to node3 knowing only its id but not dial info

	node1Local, _ := GenerateTestNode(t)
	pd := node1Local.GetRemoteNodeData()
	bs := fmt.Sprintf("%s/%s", pd.IP(), pd.ID())

	// node1 and node 2 config
	c := nodeconfig.ConfigValues
	c.SwarmConfig.Bootstrap = true
	c.SwarmConfig.RandomConnections = 2
	c.SwarmConfig.BootstrapNodes = []string{bs}

	node2Local, _ := GenerateTestNodeWithConfig(t, c)
	_, node3Remote := GenerateTestNodeWithConfig(t, c)


//	if !node2Local.GetSwarm().getRoutingTable().IsHealthy() && !node3l.GetSwarm().getRoutingTable().IsHealthy() {
//		t.Error("DHTs are not formed")
//	}
	// ping node2 -> node 3
	reqID := crypto.UUID()
	callback := make(chan SendPingResp)
	node2Local.GetPing().Register(callback)
	node2Local.GetPing().Send("hello spacemesh", reqID, node3Remote.String())

Loop:
	for {
		select {
		case c := <-callback:
			assert.Nil(t, c.err, "expected no err in response")
			if bytes.Equal(c.GetMetadata().ReqId, reqID) {
				break Loop
			}
		case <-time.After(time.Second * 30):
			t.Fatal("Timeout error - expected callback")
		}
	}
}
