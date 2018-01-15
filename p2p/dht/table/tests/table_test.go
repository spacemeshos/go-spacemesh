package tests

import (
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/dht/table"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"math/rand"
	"runtime"
	"testing"
	"time"
)

// Test basic features of the bucket struct
func TestBucket(t *testing.T) {
	const n = 100

	local := p2p.GenerateRandomNodeData()
	localId := local.DhtId()

	b := table.NewBucket()
	nodes := p2p.GenerateRandomNodesData(n)
	for i := 0; i < n; i++ {
		b.PushFront(nodes[i])
	}

	i := rand.Intn(len(nodes))
	if !b.Has(nodes[i]) {
		t.Errorf("Failed to find peer: %v", nodes[i])
	}

	spl := b.Split(0, localId)
	llist := b.List()

	for e := llist.Front(); e != nil; e = e.Next() {
		id := e.Value.(node.RemoteNodeData).DhtId()
		cpl := id.CommonPrefixLen(localId)
		if cpl > 0 {
			t.Fatalf("Split failed. found id with cpl > 0 in 0 bucket")
		}
	}

	rlist := spl.List()

	for e := rlist.Front(); e != nil; e = e.Next() {
		id := e.Value.(node.RemoteNodeData).DhtId()
		cpl := id.CommonPrefixLen(localId)
		if cpl == 0 {
			t.Fatalf("Split failed. found id with cpl == 0 in non 0 bucket")
		}
	}
}

func TestTableCallbacks(t *testing.T) {
	const n = 100
	local := p2p.GenerateRandomNodeData()
	localId := local.DhtId()

	nodes := p2p.GenerateRandomNodesData(n)

	rt := table.NewRoutingTable(10, localId)

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
			if callbackIdx == n {
				break Loop1
			}
		case <-time.After(time.Second * 5):
			t.Fatalf("Failed to get expected remove callbacks on time")
			break Loop1
		}
	}
}

// Right now, this just makes sure that it doesnt hang or crash
func TestTableUpdate(t *testing.T) {

	const n = 100
	local := p2p.GenerateRandomNodeData()
	localId := local.DhtId()

	rt := table.NewRoutingTable(20, localId)

	nodes := p2p.GenerateRandomNodesData(n)

	// Testing Update
	for i := 0; i < 10000; i++ {
		rt.Update(nodes[rand.Intn(len(nodes))])
	}

	for i := 0; i < n; i++ {

		// create a new random node
		node := p2p.GenerateRandomNodeData()

		// create callback to receive result
		callback := make(table.PeersOpChannel, 2)

		// find nearest peers
		rt.NearestPeers(table.NearestPeersReq{node.DhtId(), 5, callback})

		select {
		case c := <-callback:
			if len(c.Peers) == 0 {
				t.Fatalf("Failed to find node near %s.", node.DhtId())
			}
		case <-time.After(time.Second * 5):
			t.Fatalf("Failed to get expected update callbacks on time")
		}
	}
}

func TestTableFind(t *testing.T) {

	const n = 100

	local := p2p.GenerateRandomNodeData()
	localId := local.DhtId()

	rt := table.NewRoutingTable(20, localId)

	nodes := p2p.GenerateRandomNodesData(n)

	for i := 0; i < 5; i++ {
		rt.Update(nodes[i])
	}

	for i := 0; i < 5; i++ {

		node := nodes[i]

		//t.Logf("Searching for peer: %s...", hex.EncodeToString(node.DhtId()))

		callback := make(table.PeerOpChannel, 2)
		rt.NearestPeer(table.PeerByIdRequest{node.DhtId(), callback})

		select {
		case c := <-callback:
			if c.Peer == nil || c.Peer != node {
				t.Fatalf("Failed to lookup known node...")
			}
		case <-time.After(time.Second * 5):
			t.Fatalf("Failed to get expected nearest callbacks on time")
		}

		callback1 := make(table.PeerOpChannel, 2)
		rt.Find(table.PeerByIdRequest{node.DhtId(), callback1})

		select {
		case c := <-callback1:
			if c.Peer == nil || c.Peer != node {
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
	localId := local.DhtId()
	rt := table.NewRoutingTable(20, localId)
	nodes := p2p.GenerateRandomNodesData(n)

	for i := 0; i < n; i++ {
		rt.Update(nodes[i])
	}

	//t.Logf("Searching for peer: '%s'", nodes[2].Id())

	// create callback to receive result
	callback := make(table.PeersOpChannel, 2)

	// find nearest peers
	rt.NearestPeers(table.NearestPeersReq{nodes[2].DhtId(), i, callback})

	select {
	case c := <-callback:
		if len(c.Peers) != i {
			t.Fatal("Got unexpected number of results", len(c.Peers))
		}
	case <-time.After(time.Second * 5):
		t.Fatalf("Failed to get expected callback on time")
	}

}

func TestTableMultithreaded(t *testing.T) {

	runtime.GOMAXPROCS(runtime.NumCPU())

	const n = 5000
	const i = 15

	local := p2p.GenerateRandomNodeData()
	localId := local.DhtId()
	rt := table.NewRoutingTable(20, localId)
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
			rt.Find(table.PeerByIdRequest{nodes[n].DhtId(), nil})
		}
	}()
}

func BenchmarkUpdates(b *testing.B) {
	b.StopTimer()
	local := p2p.GenerateRandomNodeData()
	localId := local.DhtId()
	rt := table.NewRoutingTable(20, localId)
	nodes := p2p.GenerateRandomNodesData(b.N)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		rt.Update(nodes[i])
	}
}

func BenchmarkFinds(b *testing.B) {
	b.StopTimer()

	local := p2p.GenerateRandomNodeData()
	localId := local.DhtId()
	rt := table.NewRoutingTable(20, localId)
	nodes := p2p.GenerateRandomNodesData(b.N)

	for i := 0; i < b.N; i++ {
		rt.Update(nodes[i])
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		rt.Find(table.PeerByIdRequest{nodes[i].DhtId(), nil})
	}
}
