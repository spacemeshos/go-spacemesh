package dht

import (
	"math/rand"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"gopkg.in/op/go-logging.v1"

	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

func GetTestLogger(name string) *logging.Logger {
	return log.New(name, "", "").Logger
}

func TestTableCallbacks(t *testing.T) {

	const n = 100
	local := node.GenerateRandomNodeData()
	localID := local.DhtID()

	nodes := node.GenerateRandomNodesData(n)

	tlog := GetTestLogger(localID.Pretty())

	rt := NewRoutingTable(20, localID, tlog)

	for i := 0; i < n; i++ {
		rt.Update(nodes[i])
	}

	// TODO : TEST identity added callback
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
}

func TestTableUpdate(t *testing.T) {

	const n = 100
	local := node.GenerateRandomNodeData()

	localID := local.DhtID()

	rt := NewRoutingTable(10, localID, GetTestLogger(localID.Pretty()))

	nodes := node.GenerateRandomNodesData(n)

	// Testing Update
	for i := 0; i < 10000; i++ {
		rt.Update(nodes[rand.Intn(len(nodes))])
	}

	for i := 0; i < n; i++ {

		// create a new random identity
		n := node.GenerateRandomNodeData()

		// create callback to receive result
		callback := make(PeersOpChannel, 2)

		// find nearest peers to new identity
		rt.NearestPeers(NearestPeersReq{ID: n.DhtID(), Count: 5, Callback: callback})

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

	local := node.GenerateRandomNodeData()

	localID := local.DhtID()

	rt := NewRoutingTable(10, localID, GetTestLogger(localID.Pretty()))

	nodes := node.GenerateRandomNodesData(n)

	for i := 0; i < 5; i++ {
		rt.Update(nodes[i])
	}

	for i := 0; i < 5; i++ {

		n := nodes[i]

		// try to find nearest peer to n - it should be n
		callback := make(PeerOpChannel, 2)
		rt.NearestPeer(PeerByIDRequest{ID: n.DhtID(), Callback: callback})

		select {
		case c := <-callback:
			if c.Peer == node.EmptyNode || c.Peer != n {
				t.Fatalf("Failed to lookup known identity...")
			}
		case <-time.After(time.Second * 5):
			t.Fatalf("Failed to get expected nearest callbacks on time")
		}

		callback1 := make(PeerOpChannel, 2)
		rt.Find(PeerByIDRequest{ID: n.DhtID(), Callback: callback1})

		select {
		case c := <-callback1:
			if c.Peer == node.EmptyNode || c.Peer != n {
				t.Fatalf("Failed to find identity...")
			}
		case <-time.After(time.Second * 5):
			t.Fatalf("Failed to get expected find callbacks on time")
		}
	}
}

func TestTableFindCount(t *testing.T) {

	const n = 100
	const i = 15

	local := node.GenerateRandomNodeData()

	localID := local.DhtID()
	rt := NewRoutingTable(10, localID, GetTestLogger(localID.Pretty()))
	nodes := node.GenerateRandomNodesData(n)
	for i := 0; i < n; i++ {
		rt.Update(nodes[i])
	}

	// create callback to receive result
	callback := make(PeersOpChannel, 2)

	// find nearest peers
	rt.NearestPeers(NearestPeersReq{ID: nodes[2].DhtID(), Count: i, Callback: callback})

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

	local := node.GenerateRandomNodeData()
	localID := local.DhtID()
	rt := NewRoutingTable(10, localID, GetTestLogger(localID.Pretty()))
	nodes := node.GenerateRandomNodesData(n)

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
			rt.Find(PeerByIDRequest{ID: nodes[n].DhtID(), Callback: nil})
		}
	}()
}

func BenchmarkUpdates(b *testing.B) {
	b.StopTimer()
	local := node.GenerateRandomNodeData()

	localID := local.DhtID()
	rt := NewRoutingTable(10, localID, GetTestLogger(localID.Pretty()))
	nodes := node.GenerateRandomNodesData(b.N)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		rt.Update(nodes[i])
	}
}

func BenchmarkFinds(b *testing.B) {
	b.StopTimer()

	local := node.GenerateRandomNodeData()

	localID := local.DhtID()
	rt := NewRoutingTable(10, localID, GetTestLogger(localID.Pretty()))
	nodes := node.GenerateRandomNodesData(b.N)

	for i := 0; i < b.N; i++ {
		rt.Update(nodes[i])
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		findc := make(PeerOpChannel)
		rt.Find(PeerByIDRequest{ID: nodes[i].DhtID(), Callback: findc})
		<-findc
	}
}
