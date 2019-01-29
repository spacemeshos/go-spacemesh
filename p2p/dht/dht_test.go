package dht

import (
	"context"
	"fmt"
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

	d := New(ln, cfg.DiscoveryConfig, n1, cfg.RandomConnections)
	assert.NotNil(t, d, "D is not nil")
}


func bootstrapNetwork(t *testing.T, numPeers, connections int) (ids []string, dhts []*KadDHT) {
	bncfg := config.DefaultConfig()
	sim := service.NewSimulator()

	bn, _ := node.GenerateTestNode(t)
	b1 := sim.NewNodeFrom(bn.Node)
	bdht := New(bn, bncfg.DiscoveryConfig, b1, bncfg.RandomConnections)
	b1.AttachDHT(bdht)

	defaultCfg := config.DefaultConfig()
	cfg := defaultCfg.DiscoveryConfig
	cfg.Bootstrap = true
	cfg.BootstrapNodes = append(cfg.BootstrapNodes, node.StringFromNode(bn.Node))

	type all struct {
		err  error
		dht  *KadDHT
		ln   *node.LocalNode
		node service.Service
	}

	booted := make(chan all, numPeers)

	bootWaitRetrnAll := func(t *testing.T, ln *node.LocalNode, serv *service.Node, dht *KadDHT, c chan all) {
		err := dht.Bootstrap(context.TODO())
		c <- all{
			err,
			dht,
			ln,
			serv,
		}
	}

	idstoFind := make([]string, 0, numPeers)
	dhtsToLook := make([]*KadDHT, 0, numPeers)
	//selectedIds := make(map[string][]node.Node, numPeers)

	for n := 0; n < numPeers; n++ {
		ln, _ := node.GenerateTestNode(t)
		n := sim.NewNodeFrom(ln.Node)
		dht := New(ln, cfg, n, defaultCfg.RandomConnections)
		n.AttachDHT(dht)
		go bootWaitRetrnAll(t, ln, n, dht, booted)
	}

	i := 0
	for {
		if i == numPeers {
			break
		}
		b := <-booted
		if !assert.NoError(t, b.err) {
			t.FailNow()
		}
		idstoFind = append(idstoFind, b.ln.String())
		dhtsToLook = append(dhtsToLook, b.dht)

		fmt.Printf("Node %v finished bootstrap with rt of %v \r\n", b.ln.String(), b.dht.Size())
		i++
	}

	assert.Equal(t, len(idstoFind), numPeers)
	assert.Equal(t, len(dhtsToLook), numPeers)

	return idstoFind, dhtsToLook
}


func TestKadDHT_EveryNodeIsInRoutingTableAndSelected(t *testing.T) {
	numPeers := 100
	connections := 10

	idstoFind, dhtsToLook := bootstrapNetwork(t, numPeers , connections)

	var passed = make([]string, 0, numPeers)
	NL:
	for n := range idstoFind { // iterate nodes
		id := idstoFind[n]
		for j := range dhtsToLook { // iterate all selected set
			if n == j {
				continue
			}

			cb := make(PeerOpChannel)
			dhtsToLook[j].rt.NearestPeer(PeerByIDRequest{node.NewDhtIDFromBase58(id), cb})
			c := <-cb

			if c.Peer != node.EmptyNode && c.Peer.String() == id {
				passed = append(passed, id)
				continue NL
			}

		}
	}

	assert.Equal(t, len(passed), numPeers)

	selected := make(map[string][]node.Node)
	// Test Selecting
	for i :=0; i< len(dhtsToLook); i++ {
		selected[dhtsToLook[i].local.String()] = dhtsToLook[i].SelectPeers(connections)
	}


	// check everyone is selected
	passed = make([]string, 0, numPeers)
	NL2:
	for n := range idstoFind { // iterate nodes
		id := idstoFind[n]
		for j := range selected { // iterate all selected set
			if id == j {
				continue
			}

			for s := range selected[j] {
				if selected[j][s].String() == id {
					passed = append(passed, id)
					continue NL2
				}
			}
		}
	}
	// we got enough selections and we found everyone.
	assert.True(t, len(passed) >= numPeers-10)
	// select peers is random so it might not select 100% of the peers.
	// in reality we'll also relay on peer selecting us so the network will be fully connected.
}

func TestDHT_Update(t *testing.T) {
	ln, _ := node.GenerateTestNode(t)

	cfg := config.DefaultConfig()
	sim := service.NewSimulator()

	n1 := sim.NewNodeFrom(ln.Node)

	dht := New(ln, cfg.DiscoveryConfig, n1, cfg.RandomConnections)

	randnode := node.GenerateRandomNodeData()
	dht.Update(randnode)

	req := make(chan int)
	dht.rt.Size(req)
	size := <-req

	assert.Equal(t, 1, size, "Routing table filled")

	morenodes := node.GenerateRandomNodesData(config.DefaultConfig().DiscoveryConfig.RoutingTableBucketSize - 2) // more than bucketsize might result is some nodes not getting in

	for i := range morenodes {
		dht.Update(morenodes[i])
	}

	dht.rt.Size(req)
	size = <-req

	assert.Equal(t, config.DefaultConfig().DiscoveryConfig.RoutingTableBucketSize-1, size)

	evenmorenodes := node.GenerateRandomNodesData(30) // more than bucketsize might result is some nodes not getting in

	for i := range evenmorenodes {
		dht.Update(evenmorenodes[i])
	}

	dht.rt.Size(req)
	size = <-req

	assert.True(t, size > config.DefaultConfig().DiscoveryConfig.RoutingTableBucketSize, "Routing table should be at least as big as bucket size")

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

	dht := New(ln, cfg.DiscoveryConfig, n1, cfg.RandomConnections)

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

	dht := New(ln, cfg.DiscoveryConfig, n1, cfg.RandomConnections)

	randnode := node.GenerateRandomNodeData()

	dht.Update(randnode)

	ln2, _ := node.GenerateTestNode(t)

	n2 := sim.NewNodeFrom(ln2.Node)

	dht2 := New(ln2, cfg.DiscoveryConfig, n2, cfg.RandomConnections)

	dht2.Update(dht.local.Node)

	node, err := dht2.Lookup(randnode.PublicKey())
	assert.NoError(t, err, "error finding node ", err)

	assert.Equal(t, node.String(), randnode.String(), "not the same node")

}

func simNodeWithDHT(t *testing.T, sc config.DiscoveryConfig, sim *service.Simulator, connections int) (*service.Node, DHT) {
	ln, _ := node.GenerateTestNode(t)
	n := sim.NewNodeFrom(ln.Node)
	dht := New(ln, sc, n, connections)
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
	cfg := config.DefaultConfig()
	bn, _ := simNodeWithDHT(t, cfg.DiscoveryConfig, sim, cfg.RandomConnections)
	// config for other nodes
	cfg2 := config.DefaultConfig()
	cfg2.RandomConnections = 2
	cfg2.DiscoveryConfig.BootstrapNodes = []string{node.StringFromNode(bn.Node)}
	_, dht := simNodeWithDHT(t, cfg2.DiscoveryConfig, sim, cfg2.RandomConnections)
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
