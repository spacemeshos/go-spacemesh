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

	n1 := sim.NewNodeFrom(ln.NodeInfo)

	d := New(ln, cfg.SwarmConfig, n1)
	assert.NotNil(t, d, "D is not nil")
}

func simNodeWithDHT(t *testing.T, sc config.SwarmConfig, sim *service.Simulator) (*service.Node, *Discovery) {
	ln, _ := node.GenerateTestNode(t)
	n := sim.NewNodeFrom(ln.NodeInfo)
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
	cfg2.SwarmConfig.BootstrapNodes = []string{bn.String()}
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
	b1 := sim.NewNodeFrom(bn.NodeInfo)
	bdht := New(bn, bncfg.SwarmConfig, b1)

	extra, _ := node.GenerateTestNode(t)
	extrasvc := sim.NewNodeFrom(extra.NodeInfo)
	edht := New(extra, bncfg.SwarmConfig, extrasvc)
	edht.rt.AddAddress(generateDiscNode(), extra.NodeInfo)

	bdht.rt.AddAddress(extra.NodeInfo, bn.NodeInfo)

	cfg := config.DefaultConfig().SwarmConfig
	cfg.Gossip = false
	cfg.Bootstrap = true
	cfg.RandomConnections = connections
	//cfg.RoutingTableBucketSize = 2
	cfg.BootstrapNodes = append(cfg.BootstrapNodes, bn.NodeInfo.String())

	ln, _ := node.GenerateTestNode(t)
	n := sim.NewNodeFrom(ln.NodeInfo)
	dht := New(ln, cfg, n)
	err := dht.Bootstrap(context.TODO())

	require.NoError(t, err)

	res, err := bdht.rt.Lookup(ln.PublicKey())
	require.NoError(t, err)

	require.Equal(t, res.ID, ln.ID)
	require.Equal(t, res.DiscoveryPort, ln.DiscoveryPort)
	require.Equal(t, res.ProtocolPort, ln.ProtocolPort)

	res2, _ := dht.rt.Lookup(bn.PublicKey())
	//require.Error(t, err2)
	require.NotEqual(t, res2, bn)
	//bootstrap nodes are removed at the end of bootstrap

}

func TestKadDHT_BootstrapSingleBoot(t *testing.T) {
	numPeers := 100

	bncfg := config.DefaultConfig()
	sim := service.NewSimulator()

	bn, _ := node.GenerateTestNode(t)
	b1 := sim.NewNodeFrom(bn.NodeInfo)
	_ = New(bn, bncfg.SwarmConfig, b1)

	cfg := config.DefaultConfig().SwarmConfig
	cfg.Gossip = false
	cfg.Bootstrap = true
	cfg.BootstrapNodes = append(cfg.BootstrapNodes, bn.NodeInfo.String())
	cfg.RandomConnections = 8

	donech := make(chan struct{}, numPeers)

	nods, dhts := make([]*node.NodeInfo, numPeers), make([]*Discovery, numPeers)

	for i := 0; i < numPeers; i++ {
		ln, _ := node.GenerateTestNode(t)
		n := sim.NewNodeFrom(ln.NodeInfo)
		dht := New(ln, cfg, n)
		nods[i] = ln.NodeInfo
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
	numPeers := 1500
	min := 8
	bootnum := 5

	sim := service.NewSimulator()

	bncfg := config.DefaultConfig()

	cfg := config.DefaultConfig().SwarmConfig
	cfg.Gossip = false
	cfg.Bootstrap = true

	for b := 0; b < bootnum; b++ {
		bn, _ := node.GenerateTestNode(t)
		b1 := sim.NewNodeFrom(bn.NodeInfo)
		_ = New(bn, bncfg.SwarmConfig, b1)
		cfg.BootstrapNodes = append(cfg.BootstrapNodes, bn.NodeInfo.String())
	}

	cfg.RandomConnections = min

	donech := make(chan struct{}, numPeers)

	nods, dhts := make([]*node.NodeInfo, numPeers), make([]*Discovery, numPeers)

	for i := 0; i < numPeers; i++ {
		ln, _ := node.GenerateTestNode(t)
		n := sim.NewNodeFrom(ln.NodeInfo)
		dht := New(ln, cfg, n)
		nods[i] = ln.NodeInfo
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
	cfg.RoutingTableBucketSize = 1
	cfg.BootstrapNodes = []string{bsinfo.String()}
	_, dht2 := simNodeWithDHT(t, cfg, sim)

	go func() {
		<-time.After(time.Second)
		realnode := sim.NewNodeFrom(bsinfo)
		d := New(bsnode, config.DefaultConfig().SwarmConfig, realnode)
		<-time.After(time.Second)
		nd, _ := simNodeWithDHT(t, config.DefaultConfig().SwarmConfig, sim)
		d.rt.AddAddress(nd.NodeInfo, d.local.NodeInfo)
	}()

	err := dht2.Bootstrap(context.TODO())
	require.NoError(t, err)
	sz := dht2.Size()
	require.Equal(t, sz, 1)
}

func Test_Refresh(t *testing.T) {
	sim := service.NewSimulator()
	bsnode, bsinfo := node.GenerateTestNode(t)
	serv := sim.NewNodeFrom(bsinfo)

	disc := New(bsnode, config.DefaultConfig().SwarmConfig, serv)
	rt := &mockAddrBook{}
	rt.NeedNewAddressesFunc = func() bool {
		return true
	}

	requsted := 0

	refresher := &refresherMock{}

	refresher.BootstrapFunc = func(ctx context.Context, minPeers int) error {
		requsted = minPeers
		return nil
	}

	disc.rt = rt
	disc.bootstrapper = refresher

	prz := disc.SelectPeers(10)
	require.Len(t, prz, 0)
	require.Equal(t, requsted, 10)

	requsted = 0

	rt.NeedNewAddressesFunc = func() bool {
		return false
	}

	prz = disc.SelectPeers(10)
	require.Len(t, prz, 0)
	require.Equal(t, requsted, 0)
}
