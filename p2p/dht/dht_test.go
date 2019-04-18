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

	randnode := generateDiscNode()
	dht.Update(randnode)

	req := make(chan int)
	dht.rt.Size(req)
	size := <-req

	assert.Equal(t, 1, size, "Routing table filled")

	morenodes := generateDiscNodes(config.DefaultConfig().SwarmConfig.RoutingTableBucketSize - 2) // more than bucketsize might result is some nodes not getting in

	for i := range morenodes {
		dht.Update(morenodes[i])
	}

	dht.rt.Size(req)
	size = <-req

	assert.Equal(t, config.DefaultConfig().SwarmConfig.RoutingTableBucketSize-1, size)

	evenmorenodes := generateDiscNodes(30) // more than bucketsize might result is some nodes not getting in

	for i := range evenmorenodes {
		dht.Update(evenmorenodes[i])
	}

	dht.rt.Size(req)
	size = <-req

	assert.True(t, size > config.DefaultConfig().SwarmConfig.RoutingTableBucketSize, "Routing table should be at least as big as bucket size")

	lastnode := evenmorenodes[0]

	looked, err := dht.netLookup(lastnode.PublicKey())

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

	randnode := generateDiscNode()

	dht.Update(randnode)

	node, err := dht.netLookup(randnode.PublicKey())

	assert.NoError(t, err, "Should not return an error")

	assert.True(t, node.String() == randnode.String(), "should return the same node")
}

func TestDHT_Lookup2(t *testing.T) {
	ln, _ := node.GenerateTestNode(t)

	cfg := config.DefaultConfig()
	sim := service.NewSimulator()

	n1 := sim.NewNodeFrom(ln.Node)

	dht := New(ln, cfg.SwarmConfig, n1)

	randnode := generateDiscNode()

	dht.Update(randnode)

	ln2, _ := node.GenerateTestNode(t)

	n2 := sim.NewNodeFrom(ln2.Node)

	dht2 := New(ln2, cfg.SwarmConfig, n2)

	dht2.Update(discNode{dht.local.Node, dht.local.Address()})

	node, err := dht2.netLookup(randnode.PublicKey())
	require.NoError(t, err, "error finding node ", err)

	require.Equal(t, node.String(), randnode.String(), "not the same node")

}

func simNodeWithDHT(t *testing.T, sc config.SwarmConfig, sim *service.Simulator) (*service.Node, *KadDHT) {
	ln, _ := node.GenerateTestNode(t)
	n := sim.NewNodeFrom(ln.Node)
	dht := New(ln, sc, n)
	//n.AttachDHT(dht)

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
	Cancel()
	// Should return error after 2 seconds
	err := dht.Bootstrap(Ctx)
	require.EqualError(t, err, ErrBootAbort.Error(), "Should be able to abort bootstrap")
}

func Test_filterFindNodeServers(t *testing.T) {
	//func filterFindNodeServers(nodes []node.Node, queried map[string]struct{}, alpha int) []node.Node {

	nodes := generateDiscNodes(20)

	q := make(map[discNode]bool)
	// filterFindNodeServer doesn't care about activeness (the bool)
	q[nodes[0]] = false
	q[nodes[1]] = false
	q[nodes[2]] = false

	filtered := filterFindNodeServers(nodes, q, 5)

	assert.Equal(t, 5, len(filtered))

	for n := range filtered {
		if _, ok := q[filtered[n]]; ok {
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
	//bootstrap nodes are removed at the end of bootstrap
	require.Equal(t, res.Peer, emptyDiscNode)

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
			t.Fatalf("dht %d (%v) has %d peers min is %d", i, dht.local.String(), size, min)
		}
	}
	avgSize := all / len(dhts)
	if avg > 0 && avgSize < avg {
		t.Fatalf("avg rt size is %d, was expecting %d", avgSize, avg)
	}
}

func Test_findNodeFailure(t *testing.T) {
	sim := service.NewSimulator()

	bsnode, bsinfo := node.GenerateTestNode(t)

	cfg := config.DefaultConfig().SwarmConfig
	cfg.RandomConnections = 1
	cfg.RoutingTableBucketSize = 2
	cfg.BootstrapNodes = []string{node.StringFromNode(bsinfo)}
	_, dht2 := simNodeWithDHT(t, cfg, sim)

	go func() {
		<-time.After(time.Second)
		realnode := sim.NewNodeFrom(bsinfo)
		d := New(bsnode, config.DefaultConfig().SwarmConfig, realnode)
		<-time.After(time.Second)
		nd, _ := simNodeWithDHT(t, config.DefaultConfig().SwarmConfig, sim)
		d.rt.Update(discNode{nd.Node, nd.Node.Address()})
	}()

	err := dht2.Bootstrap(context.TODO())
	require.NoError(t, err)
	sz := dht2.Size()
	require.Equal(t, sz, 1)
}
