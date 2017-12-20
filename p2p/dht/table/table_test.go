package table

import (
	"github.com/UnrulyOS/go-unruly/p2p"
	"math/rand"
	"testing"
	"time"
)

// Test basic features of the bucket struct
func TestBucket(t *testing.T) {
	const n = 100

	local := p2p.GenerateRandomNodeData(t)
	localId := local.DhtId()

	b := newBucket()
	nodes := p2p.GenerateRandomNodesData(t, n)
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
		id := e.Value.(p2p.RemoteNodeData).DhtId()
		cpl := id.CommonPrefixLen(localId)
		if cpl > 0 {
			t.Fatalf("Split failed. found id with cpl > 0 in 0 bucket")
		}
	}

	rlist := spl.List()

	for e := rlist.Front(); e != nil; e = e.Next() {
		id := e.Value.(p2p.RemoteNodeData).DhtId()
		cpl := id.CommonPrefixLen(localId)
		if cpl == 0 {
			t.Fatalf("Split failed. found id with cpl == 0 in non 0 bucket")
		}
	}
}

func TestTableCallbacks(t *testing.T) {
	const n = 100
	local := p2p.GenerateRandomNodeData(t)
	localId := local.DhtId()

	nodes := p2p.GenerateRandomNodesData(t, n)

	rt := NewRoutingTable(10, localId)

	callback := make(PeerChannel, 3)
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

		case <-time.After(time.Second * 5):
			t.Fatalf("Failed to get expected update callbacks on time")
			break Loop
		}
	}

	callback = make(PeerChannel, 3)
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
	local := p2p.GenerateRandomNodeData(t)
	localId := local.DhtId()

	rt := NewRoutingTable(20, localId)

	nodes := p2p.GenerateRandomNodesData(t, n)

	// Testing Update
	for i := 0; i < 10000; i++ {
		rt.Update(nodes[rand.Intn(len(nodes))])
	}

	for i := 0; i < n; i++ {

		// create a new random node
		node := p2p.GenerateRandomNodeData(t)

		// create callback to receive result
		callback := make(PeersOpChannel, 2)

		// find nearest peers
		rt.NearestPeers(NearestPeersReq{node.DhtId(), 5, callback})

		select {
		case c := <-callback:
			if len(c.peers) == 0 {
				t.Fatal("Failed to find node near %s.", node.DhtId())
			}
		case <-time.After(time.Second * 5):
			t.Fatalf("Failed to get expected update callbacks on time")
		}
	}
}

func TestTableFind(t *testing.T) {

	const n = 100

	local := p2p.GenerateRandomNodeData(t)
	localId := local.DhtId()

	rt := NewRoutingTable(20, localId)

	nodes := p2p.GenerateRandomNodesData(t, n)

	for i := 0; i < 5; i++ {
		rt.Update(nodes[i])
	}

	for i := 0; i < 5; i++ {

		node := nodes[i]

		//t.Logf("Searching for peer: %s...", hex.EncodeToString(node.DhtId()))

		callback := make(PeerOpChannel, 2)
		rt.NearestPeer(PeerByIdRequest{node.DhtId(), callback})

		select {
		case c := <-callback:
			if c.Peer == nil || c.Peer != node {
				t.Fatalf("Failed to lookup known node...")
			}
		case <-time.After(time.Second * 5):
			t.Fatalf("Failed to get expected nearest callbacks on time")
		}

		callback1 := make(PeerOpChannel, 2)
		rt.Find(PeerByIdRequest{node.DhtId(), callback1})

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

/*
func TestTableFindMultiple(t *testing.T) {
	local := tu.RandPeerIDFatal(t)
	m := pstore.NewMetrics()
	rt := NewRoutingTable(20, ConvertPeerID(local), time.Hour, m)

	peers := make([]peer.ID, 100)
	for i := 0; i < 18; i++ {
		peers[i] = tu.RandPeerIDFatal(t)
		rt.Update(peers[i])
	}

	t.Logf("Searching for peer: '%s'", peers[2])
	found := rt.NearestPeers(ConvertPeerID(peers[2]), 15)
	if len(found) != 15 {
		t.Fatalf("Got back different number of peers than we expected.")
	}
}

// Looks for race conditions in table operations. For a more 'certain'
// test, increase the loop counter from 1000 to a much higher number
// and set GOMAXPROCS above 1
func TestTableMultithreaded(t *testing.T) {
	local := peer.ID("localPeer")
	m := pstore.NewMetrics()
	tab := NewRoutingTable(20, ConvertPeerID(local), time.Hour, m)
	var peers []peer.ID
	for i := 0; i < 500; i++ {
		peers = append(peers, tu.RandPeerIDFatal(t))
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			n := rand.Intn(len(peers))
			tab.Update(peers[n])
		}
		done <- struct{}{}
	}()

	go func() {
		for i := 0; i < 1000; i++ {
			n := rand.Intn(len(peers))
			tab.Update(peers[n])
		}
		done <- struct{}{}
	}()

	go func() {
		for i := 0; i < 1000; i++ {
			n := rand.Intn(len(peers))
			tab.Find(peers[n])
		}
		done <- struct{}{}
	}()
	<-done
	<-done
	<-done
}

func BenchmarkUpdates(b *testing.B) {
	b.StopTimer()
	local := ConvertKey("localKey")
	m := pstore.NewMetrics()
	tab := NewRoutingTable(20, local, time.Hour, m)

	var peers []peer.ID
	for i := 0; i < b.N; i++ {
		peers = append(peers, tu.RandPeerIDFatal(b))
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		tab.Update(peers[i])
	}
}

func BenchmarkFinds(b *testing.B) {
	b.StopTimer()
	local := ConvertKey("localKey")
	m := pstore.NewMetrics()
	tab := NewRoutingTable(20, local, time.Hour, m)

	var peers []peer.ID
	for i := 0; i < b.N; i++ {
		peers = append(peers, tu.RandPeerIDFatal(b))
		tab.Update(peers[i])
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		tab.Find(peers[i])
	}
}*/
