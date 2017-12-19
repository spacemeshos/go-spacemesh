package kbucket

import (
	"fmt"
	"github.com/UnrulyOS/go-unruly/p2p"
	"github.com/UnrulyOS/go-unruly/p2p/dht"
	"sort"
)

// RoutingTable manages routing to peers
// All methods visible to other packages are thread safe
// Don't call package level methods (lower-case) they are private not thread-safe

type RoutingTable interface {

	// table ops
	Update(p p2p.RemoteNodeData)       // adds a peer to the table
	Find(req PeerByIdRequest)          // find a peer by dht.ID
	NearestPeer(req PeerByIdRequest)   // nearest peer to a dht.ID
	NearestPeers(req NearestPeersReq)  // ip to n nearest peers to a dht.ID
	ListPeers(callback PeersOpChannel) // list all peers
	Size(callback chan int)            // total # of peers in the table

	// add/remove peers callbacks management - thread sfe
	RegisterPeerRemovedCallback(c PeerChannel)
	RegisterPeerAddedCallback(c PeerChannel)
	UnregisterPeerRemovedCallback(c PeerChannel)
	UnregisterPeerAddedCallback(c PeerChannel)
}

// exported helper types

type PeerOpResult struct { // result of a method that returns nil or one peer
	Peer p2p.RemoteNodeData
}
type PeerOpChannel chan *PeerOpResult // a channel that accept a peer op result

type PeersOpResult struct { // result of a method that returns 0 or more peers
	peers []p2p.RemoteNodeData
}

type PeersOpChannel chan *PeersOpResult // a channel of peers op result

type PeerChannel chan p2p.RemoteNodeData // a channel that accepts a peer

type ChannelOfPeerChannel chan PeerChannel // a channel of peer channels

type PeerByIdRequest struct { // a request by peer id that returns 0 or 1 peer
	id       dht.ID
	callback PeerOpChannel
}
type NearestPeersReq struct { // NearestPeer method req params
	id       dht.ID
	count    int
	callback PeersOpChannel
}

// RoutingTable defines the routing table.
type routingTableImpl struct {

	// ID of the local peer
	local dht.ID

	// ops impls
	findReqs         chan PeerByIdRequest
	nearestPeerReqs  chan PeerByIdRequest
	nearestPeersReqs chan NearestPeersReq
	listPeersReqs    chan PeersOpChannel
	sizeReqs         chan chan int

	updateReqs chan p2p.RemoteNodeData

	// latency metrics
	//metrics pstore.Metrics

	// Maximum acceptable latency for peers in this cluster
	//maxLatency time.Duration

	// kBuckets define all the fingers to other nodes.
	Buckets    []Bucket
	bucketsize int

	peerRemoved PeerChannel
	peerAdded   PeerChannel

	registerPeerAddedReq     ChannelOfPeerChannel
	registerPeerRemovedReq   ChannelOfPeerChannel
	unregisterPeerAddedReq   ChannelOfPeerChannel
	unregisterPeerRemovedReq ChannelOfPeerChannel

	peerRemovedCallbacks map[string]PeerChannel
	peerAddedCallbacks   map[string]PeerChannel
}

// NewRoutingTable creates a new routing table with a given bucketsize, local ID, and latency tolerance.
func NewRoutingTable(bucketsize int, localID dht.ID) RoutingTable {
	rt := &routingTableImpl{
		Buckets:     []Bucket{newBucket()},
		bucketsize:  bucketsize,
		local:       localID,
		peerRemoved: make(PeerChannel, 3),
		peerAdded:   make(PeerChannel, 3),

		findReqs:         make(chan PeerByIdRequest, 3),
		nearestPeerReqs:  make(chan PeerByIdRequest, 3),
		nearestPeersReqs: make(chan NearestPeersReq, 3),
		listPeersReqs:    make(chan PeersOpChannel, 3),
		sizeReqs:         make(chan chan int, 3),

		updateReqs: make(chan p2p.RemoteNodeData, 3),

		registerPeerAddedReq:     make(ChannelOfPeerChannel, 3),
		registerPeerRemovedReq:   make(ChannelOfPeerChannel, 3),
		unregisterPeerAddedReq:   make(ChannelOfPeerChannel, 3),
		unregisterPeerRemovedReq: make(ChannelOfPeerChannel, 3),

		peerRemovedCallbacks: make(map[string]PeerChannel),
		peerAddedCallbacks:   make(map[string]PeerChannel),
	}

	go rt.processEvents()

	return rt
}

// thread safe public interface

// Size returns the total number of peers in the routing table
func (rt *routingTableImpl) Size(callback chan int) {
	rt.sizeReqs <- callback
}

// ListPeers takes a RoutingTable and returns a list of all peers from all buckets in the table.
func (rt *routingTableImpl) ListPeers(callback PeersOpChannel) {
	rt.listPeersReqs <- callback
}

func (rt *routingTableImpl) RegisterPeerRemovedCallback(c PeerChannel) {
	rt.registerPeerRemovedReq <- c
}

func (rt *routingTableImpl) RegisterPeerAddedCallback(c PeerChannel) {
	rt.registerPeerAddedReq <- c
}

func (rt *routingTableImpl) UnregisterPeerRemovedCallback(c PeerChannel) {
	rt.unregisterPeerRemovedReq <- c
}

func (rt *routingTableImpl) UnregisterPeerAddedCallback(c PeerChannel) {
	rt.unregisterPeerAddedReq <- c
}

// Find a specific peer by ID or return nil
func (rt *routingTableImpl) Find(req PeerByIdRequest) {
	rt.findReqs <- req
}

// NearestPeer returns a single peer that is nearest to the given ID
func (rt *routingTableImpl) NearestPeer(req PeerByIdRequest) {
	rt.nearestPeerReqs <- req
}

func (rt *routingTableImpl) NearestPeers(req NearestPeersReq) {
	rt.nearestPeersReqs <- req
}

func (rt *routingTableImpl) Update(peer p2p.RemoteNodeData) {
	rt.updateReqs <- peer
}

func (rt *routingTableImpl) processEvents() {
	for {
		select {

		case p := <-rt.updateReqs:
			rt.update(p)

		case r := <-rt.sizeReqs:
			var tot int
			for _, buck := range rt.Buckets {
				tot += buck.Len()
			}
			r <- tot
		case r := <-rt.listPeersReqs:
			var peers []p2p.RemoteNodeData
			for _, buck := range rt.Buckets {
				peers = append(peers, buck.Peers()...)
			}
			r <- &PeersOpResult{peers: peers}

		case r := <-rt.nearestPeersReqs:
			peers := rt.nearestPeers(r.id, r.count)
			r.callback <- &PeersOpResult{peers: peers}

		case r := <-rt.nearestPeerReqs:
			peers := rt.nearestPeers(r.id, 1)
			if len(peers) > 0 {
				r.callback <- &PeerOpResult{peers[0]}
			}
			r.callback <- &PeerOpResult{}

		case r := <-rt.findReqs:
			peers := rt.nearestPeers(r.id, 1)
			if len(peers) == 0 || !peers[0].DhtId().Equals(r.id) {
				r.callback <- &PeerOpResult{}
			}
			r.callback <- &PeerOpResult{peers[0]}

		case p := <-rt.peerAdded:
			for _, c := range rt.peerAddedCallbacks {
				func() { c <- p }()
			}

		case p := <-rt.peerRemoved:
			for _, c := range rt.peerRemovedCallbacks {
				func() { c <- p }()
			}

		case c := <-rt.registerPeerAddedReq:
			key := pointerToString(c)
			rt.peerAddedCallbacks[key] = c

		case c := <-rt.registerPeerRemovedReq:
			key := pointerToString(c)
			rt.peerRemovedCallbacks[key] = c

		case c := <-rt.unregisterPeerAddedReq:
			key := pointerToString(c)
			delete(rt.peerAddedCallbacks, key)

		case c := <-rt.unregisterPeerRemovedReq:
			key := pointerToString(c)
			delete(rt.peerRemovedCallbacks, key)
		}
	}
}

//// non-ts private impl details below (should only be called by processEvents() and not directly

func pointerToString(p interface{}) string {
	return fmt.Sprintf("%p", p)
}

// update adds or moves the given peer to the front of its respective bucket
func (rt *routingTableImpl) update(p p2p.RemoteNodeData) {

	cpl := p.DhtId().CommonPrefixLen(rt.local)

	bucketID := cpl
	if bucketID >= len(rt.Buckets) {
		bucketID = len(rt.Buckets) - 1
	}

	bucket := rt.Buckets[bucketID]
	if bucket.Has(p) {
		// If the peer is already in the table, move it to the front.
		// This signifies that it it "more active" and the less active nodes
		// Will as a result tend towards the back of the list
		bucket.MoveToFront(p)
		return
	}

	/*
		if rt.metrics.LatencyEWMA(p) > rt.maxLatency {
			// Connection doesnt meet requirements, skip!
			return
		}*/

	// New peer, add to bucket
	bucket.PushFront(p)
	rt.peerAdded <- p

	// Are we past the max bucket size?
	if bucket.Len() > rt.bucketsize {
		// If this bucket is the rightmost bucket, and its full
		// we need to split it and create a new bucket
		if bucketID == len(rt.Buckets)-1 {
			rt.peerRemoved <- rt.nextBucket()
			return
		} else {
			// If the bucket cant split kick out least active node
			rt.peerRemoved <- bucket.PopBack()
			return
		}
	}
}

// Remove deletes a peer from the routing table. This is to be used
// when we are sure a node has disconnected completely.
func (rt *routingTableImpl) remove(p p2p.RemoteNodeData) {

	cpl := p.DhtId().CommonPrefixLen(rt.local)

	bucketID := cpl
	if bucketID >= len(rt.Buckets) {
		bucketID = len(rt.Buckets) - 1
	}

	bucket := rt.Buckets[bucketID]
	bucket.Remove(p)

	rt.peerRemoved <- p
}

func (rt *routingTableImpl) nextBucket() p2p.RemoteNodeData {
	bucket := rt.Buckets[len(rt.Buckets)-1]
	newBucket := bucket.Split(len(rt.Buckets)-1, rt.local)
	rt.Buckets = append(rt.Buckets, newBucket)
	if newBucket.Len() > rt.bucketsize {
		return rt.nextBucket()
	}

	// If all elements were on left side of split...
	if bucket.Len() > rt.bucketsize {
		return bucket.PopBack()
	}
	return nil
}

// NearestPeers returns a list of the 'count' closest peers to the given ID
func (rt *routingTableImpl) nearestPeers(id dht.ID, count int) []p2p.RemoteNodeData {

	cpl := id.CommonPrefixLen(rt.local)

	// Get bucket at cpl index or last bucket
	if cpl >= len(rt.Buckets) {
		cpl = len(rt.Buckets) - 1
	}

	bucket := rt.Buckets[cpl]

	var peerArr peerSorterArr
	peerArr = copyPeersFromList(id, peerArr, bucket.List())
	if len(peerArr) < count {
		// In the case of an unusual split, one bucket may be short or empty.
		// if this happens, search both surrounding buckets for nearby peers
		if cpl > 0 {
			plist := rt.Buckets[cpl-1].List()
			peerArr = copyPeersFromList(id, peerArr, plist)
		}

		if cpl < len(rt.Buckets)-1 {
			plist := rt.Buckets[cpl+1].List()
			peerArr = copyPeersFromList(id, peerArr, plist)
		}
	}

	// Sort by distance to local peer
	sort.Sort(peerArr)

	var out []p2p.RemoteNodeData
	for i := 0; i < count && i < peerArr.Len(); i++ {
		out = append(out, peerArr[i].node)
	}

	return out
}

// Print prints a descriptive statement about the provided RoutingTable
/*
func (rt *routingTableImpl) Print() {
	fmt.Printf("Routing Table, bs = %d, Max latency = %d\n", rt.bucketsize, rt.maxLatency)
	rt.tabLock.RLock()

	for i, b := range rt.Buckets {
		fmt.Printf("\tbucket: %d\n", i)

		b.lk.RLock()
		for e := b.list.Front(); e != nil; e = e.Next() {
			p := e.Value.(peer.ID)
			fmt.Printf("\t\t- %s %s\n", p.Pretty(), rt.metrics.LatencyEWMA(p).String())
		}
		b.lk.RUnlock()
	}
	rt.tabLock.RUnlock()
}*/
