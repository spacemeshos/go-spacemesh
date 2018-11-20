package dht

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
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

	looked, err := dht.Lookup(lastnode.PublicKey().String())

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

	node, err := dht.Lookup(randnode.PublicKey().String())

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

	node, err := dht2.Lookup(randnode.PublicKey().String())
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

func TestDHT_Bootstrap(t *testing.T) {
	// Create a bootstrap node
	sim := service.NewSimulator()
	bn, _ := simNodeWithDHT(t, config.DefaultConfig().SwarmConfig, sim)

	// config for other nodes
	cfg2 := config.DefaultConfig()
	cfg2.SwarmConfig.RandomConnections = 2 // min numbers of peers to succeed in bootstrap
	cfg2.SwarmConfig.BootstrapNodes = []string{node.StringFromNode(bn.Node)}

	booted := make(chan error)

	// boot 3 more dhts

	_, dht2 := simNodeWithDHT(t, cfg2.SwarmConfig, sim)
	_, dht3 := simNodeWithDHT(t, cfg2.SwarmConfig, sim)
	_, dht4 := simNodeWithDHT(t, cfg2.SwarmConfig, sim)

	go bootAndWait(t, dht2, booted)
	go bootAndWait(t, dht3, booted)
	go bootAndWait(t, dht4, booted)

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

	sim := service.NewSimulator()

	// Create a bootstrap node
	cfg := config.DefaultConfig()
	bn, _ := simNodeWithDHT(t, cfg.SwarmConfig, sim)

	// config for other nodes
	cfg2 := config.DefaultConfig()
	cfg2.SwarmConfig.RandomConnections = minToBoot // min numbers of peers to succeed in bootstrap
	cfg2.SwarmConfig.BootstrapNodes = []string{node.StringFromNode(bn.Node)}

	booted := make(chan error)

	for i := 0; i < nodesNum; i++ {
		_, d := simNodeWithDHT(t, cfg2.SwarmConfig, sim)
		go bootAndWait(t, d, booted)
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
	// Abort bootstrap after 2 Seconds
	go func() {
		time.Sleep(2 * time.Second)
		Cancel()
	}()
	// Should return error after 2 seconds
	err := dht.Bootstrap(Ctx)
	assert.EqualError(t, err, ErrBootAbort.Error(), "Should be able to abort bootstrap")
}

func Test_filterFindNodeServers(t *testing.T) {
	//func filterFindNodeServers(nodes []node.Node, queried map[string]struct{}, alpha int) []node.Node {

	nodes := node.GenerateRandomNodesData(20)

	q := make(map[string]struct{})
	q[nodes[0].String()] = struct{}{}
	q[nodes[1].String()] = struct{}{}
	q[nodes[2].String()] = struct{}{}

	filtered := filterFindNodeServers(nodes, q, 5)

	assert.Equal(t, 5, len(filtered))

	for n := range filtered {
		if _, ok := q[filtered[n].String()]; ok {
			t.Error("It was in the filtered")
		}
	}

}
