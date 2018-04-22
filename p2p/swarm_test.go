package p2p

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"sync/atomic"

	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
)

// Basic session test
func TestSessionCreation(t *testing.T) {

	filesystem.SetupTestSpacemeshDataFolders(t, "swarm_test")

	callback := make(chan HandshakeData)
	callback2 := make(chan HandshakeData)
	node1Local, _ := GenerateTestNode(t)
	node2Local, _ := GenerateTestNode(t)
	node1Local.GetSwarm().getHandshakeProtocol().RegisterNewSessionCallback(callback)
	node2Local.GetSwarm().getHandshakeProtocol().RegisterNewSessionCallback(callback2)
	node1Local.GetSwarm().ConnectTo(node2Local.GetRemoteNodeData())

	sessions := uint32(0)
Loop:
	for {
		s := atomic.LoadUint32(&sessions)
		if s == 2 {
			break Loop
		}
		select {
		case c := <-callback:
			if c.Session().IsAuthenticated() {
				atomic.AddUint32(&sessions, 1)
			}
		case c2 := <-callback2:
			if c2.Session().IsAuthenticated() {
				atomic.AddUint32(&sessions, 1)
			}
		case <-time.After(time.Second * 10):
			t.Fatalf("Timeout error - failed to create session")
		}
	}

	node1Local.Shutdown()
	node2Local.Shutdown()

	filesystem.DeleteSpacemeshDataFolders(t)
}

func TestMultipleSessions(t *testing.T) {

	filesystem.SetupTestSpacemeshDataFolders(t, "swarm_test")

	finishChan := make(chan struct{})
	const timeout = time.Second * 10
	const count = 500

	node1Local, rn := GenerateTestNode(t) // First node acts as bootstrap node.
	event := make(NodeEventCallback)
	node1Local.GetSwarm().RegisterNodeEventsCallback(event)

	go func() { // wait for events before starting
		i := 0
		for {
			ev := <-event
			if ev.State == SessionEstablished {
				i++
			}
			if i == count {
				break
			}
		}
		finishChan <- struct{}{}
	}()

	nodes := make([]LocalNode, count)

	for i := 0; i < count; i++ {
		n, _ := GenerateTestNode(t) // create a node
		nodes[i] = n
		n.GetSwarm().ConnectTo(rn.GetRemoteNodeData()) // connect to first node
		//nodes = append(nodes, n)
	}

	limit := time.NewTimer(timeout)

	select {
	case <-finishChan: // all nodes established.
		break
	case <-limit.C:
		t.Errorf("Could'nt establish %d sessions within a reasonable %v time", count, timeout.String())
	}

	for _, node := range nodes {
		node.Shutdown()
	}

	filesystem.DeleteSpacemeshDataFolders(t)
}

func TestSimpleBootstrap(t *testing.T) {

	filesystem.SetupTestSpacemeshDataFolders(t, "swarm_test")
	defer filesystem.DeleteSpacemeshDataFolders(t)
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
	c.SwarmConfig.RandomConnections = 1
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

func TestSmallBootstrap(t *testing.T) {

	filesystem.SetupTestSpacemeshDataFolders(t, "swarm_test")
	defer filesystem.DeleteSpacemeshDataFolders(t)

	const timeout = 20 * time.Second
	const tick = 1 * time.Second
	const randomConnections = 5
	const totalNodes = 10

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
	c.SwarmConfig.RandomConnections = randomConnections
	c.SwarmConfig.BootstrapNodes = []string{bs}

	nodes := make([]LocalNode, 0)

	for i := 0; i < totalNodes; i++ {
		n, _ := GenerateTestNodeWithConfig(t, c)
		nodes = append(nodes, n)
	}

	healthCheckNodes := func() bool {
		for _, n := range nodes {
			size := make(chan int)
			n.GetSwarm().getRoutingTable().Size(size)
			if <-size < randomConnections {
				return false
			}
		}
		return true
	}

	timer := time.NewTimer(timeout)
	ticker := time.NewTicker(tick)

HEALTH:
	for {
		select {
		case <-timer.C:
			t.Error("Failed to get healthy dht within 20 seconds")
		case <-ticker.C:
			if healthCheckNodes() {
				break HEALTH
			}
		}
	}

	for _, node := range nodes {
		node.Shutdown()
	}
}

// todo: fix me - this test is broken
func _estBasicBootstrap(t *testing.T) {

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
