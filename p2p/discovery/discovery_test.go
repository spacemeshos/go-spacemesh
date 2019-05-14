package discovery

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

const tstBootstrapTimeout = 5 * time.Minute

func TestNew(t *testing.T) {
	ln, _ := node.GenerateTestNode(t)

	cfg := config.DefaultConfig()
	sim := service.NewSimulator()

	n1 := sim.NewNodeFrom(ln.Node)

	d := New(ln, cfg.SwarmConfig, n1)
	assert.NotNil(t, d, "D is not nil")
}

func simNodeWithDHT(t *testing.T, sc config.SwarmConfig, sim *service.Simulator) (*service.Node, *Discovery) {
	ln, _ := node.GenerateTestNode(t)
	n := sim.NewNodeFrom(ln.Node)
	dht := New(ln, sc, n)
	//n.AttachDHT(discovery)

	return n, dht
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

func TestKadDHT_VerySmallBootstrap(t *testing.T) {
	connections := 1

	bncfg := config.DefaultConfig()
	sim := service.NewSimulator()

	bn, _ := node.GenerateTestNode(t)
	b1 := sim.NewNodeFrom(bn.Node)
	bdht := New(bn, bncfg.SwarmConfig, b1)

	extra, _ := node.GenerateTestNode(t)
	extrasvc := sim.NewNodeFrom(extra.Node)
	edht := New(extra, bncfg.SwarmConfig, extrasvc)
	edht.rt.AddAddress(generateDiscNode(), NodeInfoFromNode(extra.Node, extra.Node.Address()))

	bdht.rt.AddAddress(NodeInfoFromNode(extra.Node, extra.Address()), NodeInfoFromNode(bn.Node, bn.Address()))

	cfg := config.DefaultConfig().SwarmConfig
	cfg.Gossip = false
	cfg.Bootstrap = true
	cfg.RandomConnections = connections
	//cfg.RoutingTableBucketSize = 2
	cfg.BootstrapNodes = append(cfg.BootstrapNodes, node.StringFromNode(bn.Node))

	ln, _ := node.GenerateTestNode(t)
	n := sim.NewNodeFrom(ln.Node)
	dht := New(ln, cfg, n)
	err := dht.Bootstrap(context.TODO())

	require.NoError(t, err)

	res, err := bdht.rt.Lookup(ln.PublicKey())
	require.NoError(t, err)
	require.Equal(t, res.String(), ln.String())

	res2, _ := dht.rt.Lookup(bn.PublicKey())
	//require.Error(t, err2)
	require.NotEqual(t, res2.Node, bn.Node)
	//bootstrap nodes are removed at the end of bootstrap

}

func TestKadDHT_BootstrapSingleBoot(t *testing.T) {
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
	cfg.RandomConnections = 8

	donech := make(chan struct{}, numPeers)

	nods, dhts := make([]node.Node, numPeers), make([]*Discovery, numPeers)

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

	tm := time.NewTimer(tstBootstrapTimeout)

	for i := 0; i < numPeers; i++ {
		select {
		case <-donech:
			break
		case <-tm.C:
			t.Fatal("didn't boot successfully")
		}
	}

	testTables(t, dhts, 8, 10)
}

func TestKadDHT_Bootstrap(t *testing.T) {
	numPeers := 100
	min := 8
	bootnum := 5

	sim := service.NewSimulator()

	bncfg := config.DefaultConfig()

	cfg := config.DefaultConfig().SwarmConfig
	cfg.Gossip = false
	cfg.Bootstrap = true

	for b := 0; b < bootnum; b++ {
		bn, _ := node.GenerateTestNode(t)
		b1 := sim.NewNodeFrom(bn.Node)
		_ = New(bn, bncfg.SwarmConfig, b1)
		cfg.BootstrapNodes = append(cfg.BootstrapNodes, node.StringFromNode(bn.Node))
	}

	cfg.RandomConnections = min

	donech := make(chan struct{}, numPeers)

	nods, dhts := make([]node.Node, numPeers), make([]*Discovery, numPeers)

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

	tm := time.NewTimer(tstBootstrapTimeout)

	for i := 0; i < numPeers; i++ {
		select {
		case <-donech:
			break
		case <-tm.C:
			t.Fatal("didn't boot successfully")
		}
	}

	testTables(t, dhts, 8, 10)
}

func testTables(t *testing.T, dhts []*Discovery, min, avg int) {
	all := 0
	for i, dht := range dhts {
		size := dht.rt.NumAddresses()
		all += size
		if min > 0 && size < min {
			t.Fatalf("discovery %d (%v) has %d peers min is %d", i, dht.local.String(), size, min)
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
		<-time.After(time.Second / 2)
		realnode := sim.NewNodeFrom(bsinfo)
		d := New(bsnode, config.DefaultConfig().SwarmConfig, realnode)
		<-time.After(time.Second)
		nd, _ := simNodeWithDHT(t, config.DefaultConfig().SwarmConfig, sim)
		d.rt.AddAddress(NodeInfoFromNode(nd.Node, nd.Node.Address()), NodeInfoFromNode(d.local.Node, d.local.Address()))
	}()

	err := dht2.Bootstrap(context.TODO())
	require.NoError(t, err)
	sz := dht2.Size()
	require.Equal(t, sz, 1)
}
