package dht

import (
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func createTestDHT(t *testing.T, config nodeconfig.Config) *DHT {
	node, _ := node.GenerateTestNode(t)
	initRouting.Do(MsgRouting)
	p2pmock := newP2PMock(node.Node)
	return New(node, config.SwarmConfig, p2pmock)
}

func TestNew(t *testing.T) {
	node, _ := node.GenerateTestNode(t)
	MsgRouting()
	p2pmock := newP2PMock(node.Node)
	d := New(node, nodeconfig.DefaultConfig().SwarmConfig, p2pmock)
	assert.NotNil(t, d, "D is not nil")
}

func TestDHT_Update(t *testing.T) {
	dht := createTestDHT(t, nodeconfig.DefaultConfig())
	randnode := node.GenerateRandomNodeData()
	dht.Update(randnode)

	req := make(chan int)
	dht.rt.Size(req)
	size := <-req

	assert.Equal(t, 1, size, "Routing table filled")

	morenodes := node.GenerateRandomNodesData(nodeconfig.DefaultConfig().SwarmConfig.RoutingTableBucketSize - 2) // more than bucketsize might result is some nodes not getting in

	for i := range morenodes {
		dht.Update(morenodes[i])
	}

	dht.rt.Size(req)
	size = <-req

	assert.Equal(t, nodeconfig.DefaultConfig().SwarmConfig.RoutingTableBucketSize-1, size)

	evenmorenodes := node.GenerateRandomNodesData(30) // more than bucketsize might result is some nodes not getting in

	for i := range evenmorenodes {
		dht.Update(evenmorenodes[i])
	}

	dht.rt.Size(req)
	size = <-req

	assert.True(t, size > nodeconfig.DefaultConfig().SwarmConfig.RoutingTableBucketSize, "Routing table should be at least as big as bucket size")

	looked, err := dht.Lookup(randnode.PublicKey())

	assert.NoError(t, err, "error finding existing node")

	assert.True(t, looked.String() == randnode.String(), "didnt find the same node")

}

func TestDHT_Lookup(t *testing.T) {
	dht := createTestDHT(t, nodeconfig.DefaultConfig())
	randnode := node.GenerateRandomNodeData()

	dht.Update(randnode)

	node, err := dht.Lookup(randnode.PublicKey())

	assert.NoError(t, err, "Should not return an error")

	assert.True(t, node.String() == randnode.String(), "should return the same node")
}

func TestDHT_Lookup2(t *testing.T) {
	dht := createTestDHT(t, nodeconfig.DefaultConfig())

	randnode := node.GenerateRandomNodeData()

	dht.Update(randnode)

	dht2 := createTestDHT(t, nodeconfig.DefaultConfig())

	dht2.Update(dht.local.Node)

	node, err := dht2.Lookup(randnode.PublicKey())
	assert.NoError(t, err, "error finding node ", err)

	assert.Equal(t, node.String(), randnode.String(), "not the same node")

}

func TestDHT_Bootstrap(t *testing.T) {
	// Create a bootstrap node
	dht := createTestDHT(t, nodeconfig.DefaultConfig())

	// config for other nodes
	cfg2 := nodeconfig.DefaultConfig()
	cfg2.SwarmConfig.RandomConnections = 2 // min numbers of peers to succeed in bootstrap
	cfg2.SwarmConfig.BootstrapNodes = []string{node.StringFromNode(dht.local.Node)}

	booted := make(chan error)

	// Boot 3 more nodes
	dht2 := createTestDHT(t, cfg2)
	dht3 := createTestDHT(t, cfg2)
	dht4 := createTestDHT(t, cfg2)

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

	// Create a bootstrap node
	dht := createTestDHT(t, nodeconfig.DefaultConfig())

	// config for other nodes
	cfg2 := nodeconfig.DefaultConfig()
	cfg2.SwarmConfig.RandomConnections = minToBoot // min numbers of peers to succeed in bootstrap
	cfg2.SwarmConfig.BootstrapNodes = []string{node.StringFromNode(dht.local.Node)}

	booted := make(chan error)

	dhts := make([]*DHT, nodesNum)

	for i := 0; i < nodesNum; i++ {
		d := createTestDHT(t, cfg2)
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

}
