package p2p

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"sync/atomic"

	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"github.com/stretchr/testify/assert"
)

// Basic session test
func TestSessionCreation(t *testing.T) {

	filesystem.SetupTestSpacemeshDataFolders(t, "swarm_test")

	callback := make(chan HandshakeData)
	callback2 := make(chan HandshakeData)
	node1Local := p2pTestInstance(t, defaultConfig())
	node2Local := p2pTestInstance(t, defaultConfig())

	node2Remote := node2Local.LocalNode().Node

	node1Local.getHandshakeProtocol().RegisterNewSessionCallback(callback)
	node2Local.getHandshakeProtocol().RegisterNewSessionCallback(callback2)
	node1Local.ConnectTo(node2Remote, nil)

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
	defer filesystem.DeleteSpacemeshDataFolders(t)

	finishChan := make(chan struct{})
	const timeout = time.Second * 10
	const count = 500

	node1Local := p2pTestInstance(t, defaultConfig()) // First node acts as bootstrap node.
	bootnode := node1Local.LocalNode().Node
	event := make(NodeEventCallback)
	node1Local.RegisterNodeEventsCallback(event)

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

	nodes := make([]Swarm, count)

	for i := 0; i < count; i++ {
		n := p2pTestInstance(t, defaultConfig()) // create a node
		nodes[i] = n
		n.ConnectTo(bootnode, nil) // connect to first node
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

	node1Local := p2pTestInstance(t, defaultConfig())
	NewPingProtocol(node1Local)

	pd := node1Local.LocalNode().Node
	bs := fmt.Sprintf("%s/%s", pd.Address(), pd.String())

	// node1 is a bootstrap node to node2
	c := defaultConfig()
	c.SwarmConfig.Bootstrap = true
	c.SwarmConfig.RandomConnections = 1
	c.SwarmConfig.BootstrapNodes = []string{bs}

	node2Local := p2pTestInstance(t, c)

	// ping node2 -> node 1
	reqID := crypto.UUID()
	callback := make(chan SendPingResp)

	ping2 := NewPingProtocol(node2Local)
	ping2.Register(callback)

	ping2.Send("hello Spacemesh!", reqID, pd.String())

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

	const timeout = 25 * time.Second
	const randomConnections = 5
	const totalNodes = 30

	// setup:
	//
	// 1 bootstrap node
	//
	// 10 nodes using bootstrap node
	//
	// each node asks for 5 random nodes and connects to them
	// verify each node has an established session with 5 other nodes

	bnode := p2pTestInstance(t, defaultConfig())
	pd := bnode.LocalNode().Node
	bs := fmt.Sprintf("%s/%s", pd.Address(), pd.String())
	// nodes bootstrap config
	c := defaultConfig()
	c.SwarmConfig.Bootstrap = true
	c.SwarmConfig.RandomConnections = randomConnections
	c.SwarmConfig.BootstrapNodes = []string{bs}

	nodes := make([]Swarm, 0)

	nodechan := make(chan Swarm)

	for i := 0; i < totalNodes; i++ {
		go func() { ln := p2pTestInstance(t, c); nodechan <- ln }()
	}

	for n := range nodechan {
		nodes = append(nodes, n)
		if len(nodes) == totalNodes {
			close(nodechan)
		}
	}

	bootChan := make(chan error)

	for _, n := range nodes {
		go func(s Swarm) { bootChan <- s.WaitForBootstrap() }(n)
	}
	i := 0

	ti := time.NewTimer(timeout)

	for i < totalNodes-1 {
		select {
		case e := <-bootChan:
			if e != nil {
				t.Errorf("failed to boot, err:%v", e)
			}
			i++
		case <-ti.C:
			t.Errorf("Failed to bootstrap in timeout %v", timeout)
		}
	}

	for _, node := range nodes {
		node.Shutdown()
	}
}

// todo: fix me - this test is broken
func TestBasicBootstrap(t *testing.T) {
	filesystem.SetupTestSpacemeshDataFolders(t, "swarm_test")
	defer filesystem.DeleteSpacemeshDataFolders(t)
	// setup:
	// node1 - bootstrap node
	// node2 - connects to node1 on startup (bootstrap)
	// node3 - connects to node1 on startup (bootstrap)
	// node2 - sends a ping to node3 knowing only its id but not dial info

	node1Local := p2pTestInstance(t, defaultConfig())
	pd := node1Local.LocalNode().Node
	bs := fmt.Sprintf("%s/%s", pd.Address(), pd.String())

	// node1 and node 2 config
	c := nodeconfig.ConfigValues
	c.SwarmConfig.Bootstrap = true
	c.SwarmConfig.RandomConnections = 2
	c.SwarmConfig.BootstrapNodes = []string{bs}
	nchan := make(chan Swarm)
	nchan2 := make(chan Swarm)
	go func() {
		nchan <- p2pTestInstance(t, c)
	}()
	go func() {
		nchan2 <- p2pTestInstance(t, c)
	}()

	node2Local := <-nchan
	node3Local := <-nchan2

	node2Local.WaitForBootstrap()
	node3Local.WaitForBootstrap()

	ping2 := NewPingProtocol(node2Local)
	NewPingProtocol(node3Local)

	reqID := crypto.UUID()
	callback := make(chan SendPingResp)
	ping2.Register(callback)
	ping2.Send("hello Spacemesh", reqID, node3Local.LocalNode().String())

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
	node3Local.Shutdown()
}
