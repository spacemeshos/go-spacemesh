package dht

import (
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/stretchr/testify/assert"
)

var defaultBucketSize = config.DefaultConfig().SwarmConfig.RoutingTableBucketSize

func generateDiscNode() discNode {
	n := node.GenerateRandomNodeData()
	return discNode{n, n.Address()}
}

func generateDiscNodes(n int) []discNode {
	discs := make([]discNode, n)
	notdiscs := node.GenerateRandomNodesData(n)
	for i := 0; i < n; i++ {
		discs[i] = discNode{notdiscs[i], notdiscs[i].Address()}
	}
	return discs
}

func GetTestLogger(name string) log.Log {
	return log.New(name, "", "")
}

func TestTableCallbacks(t *testing.T) {

	const n = 100
	local := node.GenerateRandomNodeData()
	localID := local.DhtID()

	nodes := generateDiscNodes(n)

	tlog := GetTestLogger(localID.Pretty())

	rt := NewRoutingTable(defaultBucketSize, localID, tlog)

	for i := 0; i < n; i++ {
		rt.Update(nodes[i])
	}

	// TODO : TEST identity added callback
	// test added + removed = 100

	sizeChan := make(chan int)
	rt.Size(sizeChan)
	size := <-sizeChan // block until we have result

	if size < n/3 {
		// this test is kinda sketchy because we assume that the routing table will have at
		// least 30% of nodes. though with random generated nodes we can't really know.
		// theoretically this should never happen
		t.Error("More than 30 precent of nodes lost")
	}
}

func TestTableUpdate(t *testing.T) {

	const n = 100
	local := generateDiscNode()

	localID := local.DhtID()

	rt := NewRoutingTable(defaultBucketSize, localID, GetTestLogger(localID.Pretty()))

	nodes := generateDiscNodes(n)

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

	local := generateDiscNode()

	localID := local.DhtID()

	rt := NewRoutingTable(defaultBucketSize, localID, GetTestLogger(localID.Pretty()))

	nodes := generateDiscNodes(n)

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
			if c.Peer == emptyDiscNode || c.Peer != n {
				t.Fatalf("Failed to lookup known identity...")
			}
		case <-time.After(time.Second * 5):
			t.Fatalf("Failed to get expected nearest callbacks on time")
		}

		callback1 := make(PeerOpChannel, 2)
		rt.Find(PeerByIDRequest{ID: n.DhtID(), Callback: callback1})

		select {
		case c := <-callback1:
			if c.Peer == emptyDiscNode || c.Peer != n {
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

	local := generateDiscNode()

	localID := local.DhtID()
	rt := NewRoutingTable(defaultBucketSize, localID, GetTestLogger(localID.Pretty()))
	nodes := generateDiscNodes(n)
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

	local := generateDiscNode()
	localID := local.DhtID()
	rt := NewRoutingTable(defaultBucketSize, localID, GetTestLogger(localID.Pretty()))
	nodes := generateDiscNodes(n)

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

func TestRoutingTableImpl_SelectPeersDuplicates(t *testing.T) {
	const n = 1000
	const random = 10

	test := func(t *testing.T) {

		local := generateDiscNode()
		localID := local.DhtID()

		rt := NewRoutingTable(defaultBucketSize, localID, GetTestLogger(localID.Pretty()))

		fillRT := func() {
			nodes := generateDiscNodes(n)
			for n := range nodes {
				rt.Update(nodes[n])
			}
		}

		req := make(chan int)
		rt.Size(req)
		if <-req < random {
			fillRT()
		}

		selected := rt.SelectPeers(random)

		assert.Len(t, selected, random)
		idset := make(map[string]struct{})
		for i := 0; i < len(selected); i++ {
			if _, ok := idset[selected[i].String()]; ok {
				t.Errorf("duplicate")
			}
			assert.NotNil(t, selected[i].PublicKey())
			idset[selected[i].String()] = struct{}{}
		}
	}

	assert.True(t, t.Run("test", test))

}
func TestRoutingTableImpl_SelectPeers_EnoughPeers(t *testing.T) {
	const n = 100
	const random = 5

	ids := make(map[string]discNode)
	sids := make(map[string]RoutingTable)
	toselect := make(map[string]struct{})

	for i := 0; i < n; i++ {
		local := generateDiscNode()
		localID := local.DhtID()

		rt := NewRoutingTable(defaultBucketSize, localID, GetTestLogger(localID.Pretty()))

		ids[local.String()] = local
		sids[local.String()] = rt

	}

	var wg sync.WaitGroup

	for _, id := range ids {
		id := id
		for _, secondID := range ids {
			if id.DhtID().Equals(secondID.DhtID()) {
				continue
			}
			wg.Add(1)
			go func(id, secondID discNode) {
				sids[id.String()].Update(secondID)
				wg.Done()
			}(id, secondID)
		}
	}

	wg.Wait()

	for rtid := range sids {
		sel := sids[rtid].SelectPeers(random)
		assert.NotNil(t, sel)
		for ns := range sel {
			toselect[sel[ns].String()] = struct{}{}
		}

	}

	assert.True(t, len(toselect) > int(n-n*0.1)) // almost every node got selected
}

func TestRoutingTableImpl_Print(t *testing.T) {
	local := node.GenerateRandomNodeData()
	localID := local.DhtID()
	rt := NewRoutingTable(defaultBucketSize, localID, GetTestLogger(localID.Pretty()))
	rt.Print()
}

func TestRoutingTableImpl_Remove(t *testing.T) {
	local := node.GenerateRandomNodeData()
	localID := local.DhtID()
	rt := NewRoutingTable(defaultBucketSize, localID, GetTestLogger(localID.Pretty()))

	rnode := generateDiscNode()

	rt.Update(rnode)

	cnode := make(PeerOpChannel)
	rt.Find(PeerByIDRequest{rnode.DhtID(), cnode})
	n := <-cnode
	require.NotEqual(t, n.Peer, emptyDiscNode)

	rt.Remove(rnode)

	rt.Find(PeerByIDRequest{rnode.DhtID(), cnode})
	n = <-cnode
	require.Equal(t, n.Peer, emptyDiscNode)
}

func BenchmarkUpdates(b *testing.B) {
	b.StopTimer()
	local := generateDiscNode()

	localID := local.DhtID()
	rt := NewRoutingTable(defaultBucketSize, localID, GetTestLogger(localID.Pretty()))
	nodes := generateDiscNodes(b.N)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		rt.Update(nodes[i])
	}
}

func BenchmarkFinds(b *testing.B) {
	b.StopTimer()

	local := generateDiscNode()

	localID := local.DhtID()
	rt := NewRoutingTable(defaultBucketSize, localID, GetTestLogger(localID.Pretty()))
	nodes := generateDiscNodes(b.N)

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
