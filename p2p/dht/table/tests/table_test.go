package tests

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/dht/table"
	"math/rand"
	"testing"
	"time"
)

func TestTableCallbacks(t *testing.T) {
	const n = 100
	local := p2p.GenerateRandomNodeData()
	localID := local.DhtID()

	nodes := p2p.GenerateRandomNodesData(n)

	rt := table.NewRoutingTable(10, localID)

	callback := make(table.PeerChannel, 3)
	callbackIdx := 0
	rt.RegisterPeerAddedCallback(callback)

	for i := 0; i < n; i++ {
		rt.Update(nodes[i])
	}

Loop:
	for {
		select {
		case <-callback:
			callbackIdx++
			if callbackIdx == n {
				break Loop
			}

		case <-time.After(time.Second * 10):
			t.Fatalf("Failed to get expected update callbacks on time")
			break Loop
		}
	}

	sizeChan := make(chan int)
	size := 0

	rt.Size(sizeChan)

	select {
	case s := <-sizeChan:
		size = s
		log.Info("%d nodes in table", size)
		//assert.True(t, s == n, fmt.Sprintf("unexpected nubmer of items %d, %d", s, n))
	}

	callback = make(table.PeerChannel, 3)
	callbackIdx = 0
	rt.RegisterPeerRemovedCallback(callback)

	for i := 0; i < n; i++ {
		rt.Remove(nodes[i])
	}

Loop1:
	for {
		select {
		case <-callback:
			callbackIdx++
			if callbackIdx == size {
				break Loop1
			}
		case <-time.After(time.Second * 10):
			t.Fatalf("Failed to get expected remove callbacks on time")
			break Loop1
		}
	}
}

func TestTableUpdate(t *testing.T) {

	const n = 100
	local := p2p.GenerateRandomNodeData()
	localID := local.DhtID()

	rt := table.NewRoutingTable(20, localID)

	nodes := p2p.GenerateRandomNodesData(n)

	// Testing Update
	for i := 0; i < 10000; i++ {
		rt.Update(nodes[rand.Intn(len(nodes))])
	}

	for i := 0; i < n; i++ {

		// create a new random node
		n := p2p.GenerateRandomNodeData()

		// create callback to receive result
		callback := make(table.PeersOpChannel, 2)

		// find nearest peers to new node
		rt.NearestPeers(table.NearestPeersReq{ID: n.DhtID(), Count: 5, Callback: callback})

		select {
		case c := <-callback:
			if len(c.Peers) != 5 {
				t.Fatalf("Expected to find 5 close nodes to %s.", n.DhtID())
			}
		case <-time.After(time.Second * 5):
			t.Fatalf("Failed to get expected update callbacks on time")
		}
	}
}

func TestTableFind(t *testing.T) {

	const n = 100

	local := p2p.GenerateRandomNodeData()
	localID := local.DhtID()

	rt := table.NewRoutingTable(20, localID)

	nodes := p2p.GenerateRandomNodesData(n)

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

	local := p2p.GenerateRandomNodeData()
	localID := local.DhtID()
	rt := table.NewRoutingTable(20, localID)
	nodes := p2p.GenerateRandomNodesData(n)

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

	local := p2p.GenerateRandomNodeData()
	localID := local.DhtID()
	rt := table.NewRoutingTable(20, localID)
	nodes := p2p.GenerateRandomNodesData(n)

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
	local := p2p.GenerateRandomNodeData()
	localID := local.DhtID()
	rt := table.NewRoutingTable(20, localID)
	nodes := p2p.GenerateRandomNodesData(b.N)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		rt.Update(nodes[i])
	}
}

func BenchmarkFinds(b *testing.B) {
	b.StopTimer()

	local := p2p.GenerateRandomNodeData()
	localID := local.DhtID()
	rt := table.NewRoutingTable(20, localID)
	nodes := p2p.GenerateRandomNodesData(b.N)

	for i := 0; i < b.N; i++ {
		rt.Update(nodes[i])
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		rt.Find(table.PeerByIDRequest{ID: nodes[i].DhtID(), Callback: nil})
	}
}
