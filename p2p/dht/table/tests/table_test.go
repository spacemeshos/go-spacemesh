package tests

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/dht/table"
	"gopkg.in/op/go-logging.v1"
	//"fmt"
)

func GetTestLogger(name string) *logging.Logger {
	return log.CreateLogger(name, "", "")
}

func TestTableCallbacks(t *testing.T) {

	const n = 100
	local, err := p2p.GenerateRandomNodeData()

	assert.NoError(t, err, "Should be able to create a node")

	localID := local.DhtID()

	nodes, err := p2p.GenerateRandomNodesData(n)

	assert.NoError(t, err, "Should be able to create multiple nodes")

	log := GetTestLogger(localID.Pretty())

	rt := table.NewRoutingTable(20, localID, log)

	for i := 0; i < n; i++ {
		rt.Update(nodes[i])
	}

	// TODO : TEST node added callback
	// test added + removed = 100

	sizeChan := make(chan int)
	rt.Size(sizeChan)
	size := <-sizeChan // block until we have result

	if size < 50 {
		// this test is kinda sketchy because we assume that the routing table will have at
		// least 50% of nodes. though with random generated nodes we can't really know.
		// theoretically this should never happen
		t.Error("More than 50 precent of nodes lost")
	}

	callback := make(table.PeerChannel, 3)
	callbackIdx := 0
	rt.RegisterPeerRemovedCallback(callback)

	for i := 0; i < n; i++ {
		rt.Remove(nodes[i])
	}

Loop:
	for {
		select {
		case <-callback:
			callbackIdx++
			if callbackIdx == size {
				break Loop
			}
		case <-time.After(time.Second * 10):
			t.Fatalf("Failed to get expected remove callbacks on time")
			break Loop
		}
	}

}

func TestTableUpdate(t *testing.T) {

	const n = 100
	local, err := p2p.GenerateRandomNodeData()
	assert.NoError(t, err, "Should be able to create a node")

	localID := local.DhtID()

	rt := table.NewRoutingTable(10, localID, GetTestLogger(localID.Pretty()))

	nodes, err := p2p.GenerateRandomNodesData(n)
	assert.NoError(t, err, "Should be able to create multiple nodes")

	// Testing Update
	for i := 0; i < 10000; i++ {
		rt.Update(nodes[rand.Intn(len(nodes))])
	}

	for i := 0; i < n; i++ {

		// create a new random node
		n, err := p2p.GenerateRandomNodeData()
		assert.NoError(t, err, "Should be able to create a node")

		// create callback to receive result
		callback := make(table.PeersOpChannel, 2)

		// find nearest peers to new node
		rt.NearestPeers(table.NearestPeersReq{ID: n.DhtID(), Count: 5, Callback: callback})

		select {
		case c := <-callback:
			if len(c.Peers) != 5 {
				t.Fatalf("Expected to find 5 close nodes to %s.", n.DhtID().Pretty())
			}
		case <-time.After(time.Second * 5):
			t.Fatalf("Failed to get expected update callbacks on time")
		}
	}
}

func TestTableFind(t *testing.T) {

	const n = 100

	local, err := p2p.GenerateRandomNodeData()
	assert.NoError(t, err, "Should be able to create a node")

	localID := local.DhtID()

	rt := table.NewRoutingTable(10, localID, GetTestLogger(localID.Pretty()))

	nodes, err := p2p.GenerateRandomNodesData(n)
	assert.NoError(t, err, "Should be able to create a node")

	for i := 0; i < 5; i++ {
		rt.Update(nodes[i])
	}

	for i := 0; i < 5; i++ {

		n := nodes[i]

		// try to find nearest peer to n - it should be n
		callback := make(table.PeerOpChannel, 2)
		rt.NearestPeer(table.PeerByIDRequest{ID: n.DhtID(), Callback: callback})

		select {
		case c := <-callback:
			if c.Peer == nil || c.Peer != n {
				t.Fatalf("Failed to lookup known node...")
			}
		case <-time.After(time.Second * 5):
			t.Fatalf("Failed to get expected nearest callbacks on time")
		}

		callback1 := make(table.PeerOpChannel, 2)
		rt.Find(table.PeerByIDRequest{ID: n.DhtID(), Callback: callback1})

		select {
		case c := <-callback1:
			if c.Peer == nil || c.Peer != n {
				t.Fatalf("Failed to find node...")
			}
		case <-time.After(time.Second * 5):
			t.Fatalf("Failed to get expected find callbacks on time")
		}
	}
}

func TestTableFindCount(t *testing.T) {

	const n = 100
	const i = 15

	local, err := p2p.GenerateRandomNodeData()
	assert.NoError(t, err, "Should be able to create a node")

	localID := local.DhtID()
	rt := table.NewRoutingTable(10, localID, GetTestLogger(localID.Pretty()))
	nodes, err := p2p.GenerateRandomNodesData(n)
	assert.NoError(t, err, "Should be able to create a node")

	for i := 0; i < n; i++ {
		rt.Update(nodes[i])
	}

	// create callback to receive result
	callback := make(table.PeersOpChannel, 2)

	// find nearest peers
	rt.NearestPeers(table.NearestPeersReq{ID: nodes[2].DhtID(), Count: i, Callback: callback})

	select {
	case c := <-callback:
		if len(c.Peers) != i {
			t.Fatal("Got unexpected number of results", len(c.Peers))
		}
	case <-time.After(time.Second * 5):
		t.Fatalf("Failed to get expected callback on time")
	}

}

func TestTableMultiThreaded(t *testing.T) {

	const n = 5000
	const i = 15

	local, err := p2p.GenerateRandomNodeData()
	assert.NoError(t, err, "Should be able to create a node")

	localID := local.DhtID()
	rt := table.NewRoutingTable(10, localID, GetTestLogger(localID.Pretty()))
	nodes, err := p2p.GenerateRandomNodesData(n)
	assert.NoError(t, err, "Should be able to create multiple nodes")

	go func() {
		for i := 0; i < 1000; i++ {
			n := rand.Intn(len(nodes))
			rt.Update(nodes[n])
		}
	}()

	go func() {
		for i := 0; i < 1000; i++ {
			n := rand.Intn(len(nodes))
			rt.Update(nodes[n])
		}
	}()

	go func() {
		for i := 0; i < 1000; i++ {
			n := rand.Intn(len(nodes))
			rt.Find(table.PeerByIDRequest{ID: nodes[n].DhtID(), Callback: nil})
		}
	}()
}

func BenchmarkUpdates(b *testing.B) {
	b.StopTimer()
	local, err := p2p.GenerateRandomNodeData()
	if err != nil {
		b.Errorf("Should be able to create node error:%v", err)
	}

	localID := local.DhtID()
	rt := table.NewRoutingTable(10, localID, GetTestLogger(localID.Pretty()))
	nodes, err := p2p.GenerateRandomNodesData(b.N)
	if err != nil {
		b.Errorf("Should be able to create multiple error:%v", err)
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		rt.Update(nodes[i])
	}
}

func BenchmarkFinds(b *testing.B) {
	b.StopTimer()

	local, err := p2p.GenerateRandomNodeData()
	if err != nil {
		b.Errorf("Should be able to create node error:%v", err)
	}

	localID := local.DhtID()
	rt := table.NewRoutingTable(10, localID, GetTestLogger(localID.Pretty()))
	nodes, err := p2p.GenerateRandomNodesData(b.N)
	if err != nil {
		b.Errorf("Should be able to create node error:%v", err)
	}

	for i := 0; i < b.N; i++ {
		rt.Update(nodes[i])
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		rt.Find(table.PeerByIDRequest{ID: nodes[i].DhtID(), Callback: nil})
	}
}
