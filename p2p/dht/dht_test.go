package dht

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	ln, _ := node.GenerateTestNode(t)

	cfg := config.DefaultConfig()
	sim := service.NewSimulator()

	n1 := sim.NewNodeFrom(ln.Node)

	d := New(ln, cfg.SwarmConfig, n1)
	assert.NotNil(t, d, "D is not nil")
}

func TestDHT_Update(t *testing.T) {
	ln, _ := node.GenerateTestNode(t)

	cfg := config.DefaultConfig()
	sim := service.NewSimulator()

	n1 := sim.NewNodeFrom(ln.Node)

	dht := New(ln, cfg.SwarmConfig, n1)

	randnode := node.GenerateRandomNodeData()
	dht.Update(randnode)

	req := make(chan int)
	dht.rt.Size(req)
	size := <-req

	assert.Equal(t, 1, size, "Routing table filled")

	morenodes := node.GenerateRandomNodesData(config.DefaultConfig().SwarmConfig.RoutingTableBucketSize - 2) // more than bucketsize might result is some nodes not getting in

	for i := range morenodes {
		dht.Update(morenodes[i])
	}

	dht.rt.Size(req)
	size = <-req

	assert.Equal(t, config.DefaultConfig().SwarmConfig.RoutingTableBucketSize-1, size)

	evenmorenodes := node.GenerateRandomNodesData(30) // more than bucketsize might result is some nodes not getting in

	for i := range evenmorenodes {
		dht.Update(evenmorenodes[i])
	}

	dht.rt.Size(req)
	size = <-req

	assert.True(t, size > config.DefaultConfig().SwarmConfig.RoutingTableBucketSize, "Routing table should be at least as big as bucket size")

	lastnode := evenmorenodes[0]

	looked, err := dht.Lookup(lastnode.PublicKey())

	assert.NoError(t, err, "error finding existing node ")

	assert.Equal(t, looked.String(), lastnode.String(), "didnt find the same node")
	assert.Equal(t, looked.Address(), lastnode.Address(), "didnt find the same node")

}

func TestDHT_Lookup(t *testing.T) {
	ln, _ := node.GenerateTestNode(t)

	cfg := config.DefaultConfig()
	sim := service.NewSimulator()

	n1 := sim.NewNodeFrom(ln.Node)

	dht := New(ln, cfg.SwarmConfig, n1)

	randnode := node.GenerateRandomNodeData()

	dht.Update(randnode)

	node, err := dht.Lookup(randnode.PublicKey())

	assert.NoError(t, err, "Should not return an error")

	assert.True(t, node.String() == randnode.String(), "should return the same node")
}

func TestDHT_Lookup2(t *testing.T) {
	ln, _ := node.GenerateTestNode(t)

	cfg := config.DefaultConfig()
	sim := service.NewSimulator()

	n1 := sim.NewNodeFrom(ln.Node)

	dht := New(ln, cfg.SwarmConfig, n1)

	randnode := node.GenerateRandomNodeData()

	dht.Update(randnode)

	ln2, _ := node.GenerateTestNode(t)

	n2 := sim.NewNodeFrom(ln2.Node)

	dht2 := New(ln2, cfg.SwarmConfig, n2)

	dht2.Update(dht.local.Node)

	node, err := dht2.Lookup(randnode.PublicKey())
	assert.NoError(t, err, "error finding node ", err)

	assert.Equal(t, node.String(), randnode.String(), "not the same node")

}

func simNodeWithDHT(t *testing.T, sc config.SwarmConfig, sim *service.Simulator) (*service.Node, DHT) {
	ln, _ := node.GenerateTestNode(t)
	n := sim.NewNodeFrom(ln.Node)
	dht := New(ln, sc, n)
	n.AttachDHT(dht)

	return n, dht
}

func bootAndWait(t *testing.T, dht DHT, errchan chan error) {
	err := dht.Bootstrap(context.TODO())
	errchan <- err
}

func TestDHT_BootstrapAbort(t *testing.T) {
	// Create a bootstrap node
	sim := service.NewSimulator()
	bn, _ := simNodeWithDHT(t, config.DefaultConfig().SwarmConfig, sim)
	// config for other nodes
	cfg2 := config.DefaultConfig()
	cfg2.SwarmConfig.RandomConnections = 2
	cfg2.SwarmConfig.BootstrapNodes = []string{node.StringFromNode(bn.Node)}
	_, dht := simNodeWithDHT(t, cfg2.SwarmConfig, sim)
	// Create a bootstrap node to abort
	Ctx, Cancel := context.WithCancel(context.Background())
	// Abort bootstrap after 500 milliseconds
	go func() {
		time.Sleep(500 * time.Millisecond)
		Cancel()
	}()
	// Should return error after 2 seconds
	err := dht.Bootstrap(Ctx)
	assert.EqualError(t, err, ErrBootAbort.Error(), "Should be able to abort bootstrap")
}

func Test_filterFindNodeServers(t *testing.T) {
	//func filterFindNodeServers(nodes []node.Node, queried map[string]struct{}, alpha int) []node.Node {

	nodes := node.GenerateRandomNodesData(20)

	q := make(map[string]bool)
	// filterFindNodeServer doesn't care about activeness (the bool)
	q[nodes[0].String()] = false
	q[nodes[1].String()] = false
	q[nodes[2].String()] = false

	filtered := filterFindNodeServers(nodes, q, 5)

	assert.Equal(t, 5, len(filtered))

	for n := range filtered {
		if _, ok := q[filtered[n].String()]; ok {
			t.Error("It was in the filtered")
		}
	}

}

func TestKadDHT_VerySmallBootstrap(t *testing.T) {
	connections := 1

	bncfg := config.DefaultConfig()
	sim := service.NewSimulator()

	bn, _ := node.GenerateTestNode(t)
	b1 := sim.NewNodeFrom(bn.Node)
	bdht := New(bn, bncfg.SwarmConfig, b1)

	cfg := config.DefaultConfig().SwarmConfig
	cfg.Gossip = false
	cfg.Bootstrap = true
	cfg.RandomConnections = connections
	cfg.RoutingTableBucketSize = 1
	cfg.BootstrapNodes = append(cfg.BootstrapNodes, node.StringFromNode(bn.Node))

	ln, _ := node.GenerateTestNode(t)
	n := sim.NewNodeFrom(ln.Node)
	dht := New(ln, cfg, n)
	err := dht.Bootstrap(context.TODO())

	require.NoError(t, err)

	cb := make(PeerOpChannel)
	bdht.rt.NearestPeer(PeerByIDRequest{ID: ln.DhtID(), Callback: cb})
	res := <-cb

	require.Equal(t, res.Peer.String(), ln.String())

	cb2 := make(PeerOpChannel)
	dht.rt.NearestPeer(PeerByIDRequest{ID: bn.DhtID(), Callback: cb2})
	res = <-cb2
	require.Equal(t, res.Peer.String(), bn.String())

}

func TestKadDHT_Bootstrap(t *testing.T) {
	numPeers := 100

	bncfg := config.DefaultConfig()
	sim := service.NewSimulator()

	bn, _ := node.GenerateTestNode(t)
	b1 := sim.NewNodeFrom(bn.Node)
	_ = New(bn, bncfg.SwarmConfig, b1)

	cfg := config.DefaultConfig().SwarmConfig
	cfg.Gossip = false
	cfg.Bootstrap = true
	cfg.BootstrapNodes = append(cfg.BootstrapNodes, node.StringFromNode(bn.Node))
	cfg.RandomConnections = numPeers / 3

	donech := make(chan struct{}, numPeers)

	nods, dhts := make([]node.Node, numPeers), make([]*KadDHT, numPeers)

	for i := 0; i < numPeers; i++ {
		ln, _ := node.GenerateTestNode(t)
		n := sim.NewNodeFrom(ln.Node)
		dht := New(ln, cfg, n)
		nods[i] = ln.Node
		dhts[i] = dht
		go func() {
			err := dht.Bootstrap(context.TODO())
			require.NoError(t, err)
			donech <- struct{}{}
		}()
	}

	tm := time.NewTimer(BootstrapTimeout)

	for i := 0; i < numPeers; i++ {
		select {
		case <-donech:
			break
		case <-tm.C:
			t.Fatal("didn't boot successfully")
		}
	}

	testDHTs(t, dhts, defaultBucketSize, numPeers/3)
}

func testDHTs(t *testing.T, dhts []*KadDHT, min, avg int) {
	all := 0
	for i, dht := range dhts {
		cb := make(chan int)
		dht.rt.Size(cb)
		size := <-cb
		all += size
		if min > 0 && size < min {
			t.Fatalf("dht %d has %d peers min is %d", i, size, min)
		}
	}
	avgSize := all / len(dhts)
	if avg > 0 && avgSize < avg {
		t.Fatalf("avg rt size is %d, was expecting %d", avgSize, avg)
	}
}
