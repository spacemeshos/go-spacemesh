package dht

import (
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/simulator"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	ln, _ := node.GenerateTestNode(t)

	cfg := config.DefaultConfig()
	sim := simulator.New()

	n1 := sim.NewNodeFrom(ln.Node)

	d := New(ln, cfg.SwarmConfig, n1)
	assert.NotNil(t, d, "D is not nil")
}

func TestDHT_Update(t *testing.T) {
	ln, _ := node.GenerateTestNode(t)

	cfg := config.DefaultConfig()
	sim := simulator.New()

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

	looked, err := dht.Lookup(randnode.PublicKey().String())

	assert.NoError(t, err, "error finding existing node")

	assert.True(t, looked.String() == randnode.String(), "didnt find the same node")

}

func TestDHT_Lookup(t *testing.T) {
	ln, _ := node.GenerateTestNode(t)

	cfg := config.DefaultConfig()
	sim := simulator.New()

	n1 := sim.NewNodeFrom(ln.Node)

	dht := New(ln, cfg.SwarmConfig, n1)

	randnode := node.GenerateRandomNodeData()

	dht.Update(randnode)

	node, err := dht.Lookup(randnode.PublicKey().String())

	assert.NoError(t, err, "Should not return an error")

	assert.True(t, node.String() == randnode.String(), "should return the same node")
}

func TestDHT_Lookup2(t *testing.T) {
	ln, _ := node.GenerateTestNode(t)

	cfg := config.DefaultConfig()
	sim := simulator.New()

	n1 := sim.NewNodeFrom(ln.Node)

	dht := New(ln, cfg.SwarmConfig, n1)

	randnode := node.GenerateRandomNodeData()

	dht.Update(randnode)

	ln2, _ := node.GenerateTestNode(t)

	n2 := sim.NewNodeFrom(ln2.Node)

	dht2 := New(ln2, cfg.SwarmConfig, n2)

	dht2.Update(dht.local.Node)

	node, err := dht2.Lookup(randnode.PublicKey().String())
	assert.NoError(t, err, "error finding node ", err)

	assert.Equal(t, node.String(), randnode.String(), "not the same node")

}

func TestDHT_Bootstrap(t *testing.T) {
	// Create a bootstrap node
	ln, _ := node.GenerateTestNode(t)
	cfg := config.DefaultConfig()
	sim := simulator.New()
	n1 := sim.NewNodeFrom(ln.Node)

	dht := New(ln, cfg.SwarmConfig, n1)

	// config for other nodes
	cfg2 := config.DefaultConfig()
	cfg2.SwarmConfig.RandomConnections = 2 // min numbers of peers to succeed in bootstrap
	cfg2.SwarmConfig.BootstrapNodes = []string{node.StringFromNode(dht.local.Node)}

	booted := make(chan error)

	// Boot 3 more nodes
	ln2, _ := node.GenerateTestNode(t)
	n2 := sim.NewNodeFrom(ln2.Node)
	dht2 := New(ln2, cfg2.SwarmConfig, n2)

	ln3, _ := node.GenerateTestNode(t)
	n3 := sim.NewNodeFrom(ln3.Node)
	dht3 := New(ln3, cfg2.SwarmConfig, n3)

	ln4, _ := node.GenerateTestNode(t)
	n4 := sim.NewNodeFrom(ln4.Node)
	dht4 := New(ln4, cfg2.SwarmConfig, n4)

	go func() {
		err2 := dht2.Bootstrap()
		booted <- err2
	}()
	go func() {
		err3 := dht3.Bootstrap()
		booted <- err3
	}()
	go func() {
		err4 := dht4.Bootstrap()
		booted <- err4
	}()

	// Collect errors
	err := <-booted
	assert.NoError(t, err, "should be able to bootstrap a node")
	err = <-booted
	assert.NoError(t, err, "should be able to bootstrap another node")
	err = <-booted
	assert.NoError(t, err, "should be able to bootstrap another node")
}

// A bigger bootstrap
func TestDHT_Bootstrap2(t *testing.T) {

	const timeout = 10 * time.Second
	const nodesNum = 100
	const minToBoot = 25

	sim := simulator.New()

	// Create a bootstrap node
	bs, _ := node.GenerateTestNode(t)
	cfg := config.DefaultConfig()
	bsn := sim.NewNodeFrom(bs.Node)

	dht := New(bs, cfg.SwarmConfig, bsn)

	// config for other nodes
	cfg2 := config.DefaultConfig()
	cfg2.SwarmConfig.RandomConnections = minToBoot // min numbers of peers to succeed in bootstrap
	cfg2.SwarmConfig.BootstrapNodes = []string{node.StringFromNode(dht.local.Node)}

	booted := make(chan error)

	dhts := make([]*DHT, nodesNum)

	for i := 0; i < nodesNum; i++ {
		lln, _ := node.GenerateTestNode(t)
		nn := sim.NewNodeFrom(lln.Node)

		d := New(lln, cfg2.SwarmConfig, nn)

		dhts[i] = d
		go func(d *DHT) { err := d.Bootstrap(); booted <- err }(d)
	}

	timer := time.NewTimer(timeout)

	i := 0
	for i < nodesNum-1 {
		select {
		case e := <-booted:
			if e != nil {
				t.Error("Failed to boot a node")
			}
			i++
		case <-timer.C:
			t.Error("Failed to boot within time")
		}
	}

	time.Sleep(1 * time.Second)

}
