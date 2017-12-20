package table

import (
	"fmt"
	"github.com/UnrulyOS/go-unruly/p2p/dht"
	"github.com/UnrulyOS/go-unruly/p2p/node"
	"sort"
)

// RoutingTable manages routing to peers
// All methods visible to externals packages are thread-safe
// Don't call package-level methods (lower-case) - they are private not thread-safe
// Design spec: 'Kademlia: A Design Specification' with most recently active nodes at the top of each bucket
type RoutingTable interface {

	// table ops
	Update(p node.RemoteNodeData)      // adds a peer to the table
	Remove(p node.RemoteNodeData)      // remove a peer from the table
	Find(req PeerByIdRequest)          // find a peer by dht.ID
	NearestPeer(req PeerByIdRequest)   // nearest peer to a dht.ID
	NearestPeers(req NearestPeersReq)  // ip to n nearest peers to a dht.ID
	ListPeers(callback PeersOpChannel) // list all peers
	Size(callback chan int)            // total # of peers in the table

	// add/remove peers callbacks management - thread sfe
	RegisterPeerRemovedCallback(c PeerChannel)   // get called when a  peer is removed
	RegisterPeerAddedCallback(c PeerChannel)     // get called when a peer is added
	UnregisterPeerRemovedCallback(c PeerChannel) // remove addition reg
	UnregisterPeerAddedCallback(c PeerChannel)   // remove removal reg
}

// exported helper types

type PeerOpResult struct { // result of a method that returns nil or one peer
	Peer node.RemoteNodeData
}
type PeerOpChannel chan *PeerOpResult // a channel that accept a peer op result

type PeersOpResult struct { // result of a method that returns 0 or more peers
	Peers []node.RemoteNodeData
}

type PeersOpChannel chan *PeersOpResult // a channel of peers op result

type PeerChannel chan node.RemoteNodeData // a channel that accepts a peer

type ChannelOfPeerChannel chan PeerChannel // a channel of peer channels

type PeerByIdRequest struct { // a request by peer id that returns 0 or 1 peer
	Id       dht.ID
	Callback PeerOpChannel
}
type NearestPeersReq struct { // NearestPeer method req params
	Id       dht.ID
	Count    int
	Callback PeersOpChannel
}

// RoutingTable defines the routing table.
// Most recently network active nodes are placed in beginning of buckets.
// Least active nodes are the back of each bucket.
// Bucket index is the size of the common prefix of nodes in that buckets and the local node
// l:  0 1 0 0 1 1
// n1: 0 0 1 1 0 1
// n2: 0 1 0 1 1 1
// dist(l,n1) = xor(l,n1) = 0 1 1 1 1 0
// dist(l,n2) = xor(l,n2) = 0 0 0 1 0 0
// clp(l,n1) = 1 -> n1 => bucket[1]
// clp(l,n2) = 3 -> n2 => bucket[3]
// Closer nodes will appear in buckets with a higher index
type routingTableImpl struct {

	// local peer ID
	local dht.ID

	// ops impls
	findReqs         chan PeerByIdRequest
	nearestPeerReqs  chan PeerByIdRequest
	nearestPeersReqs chan NearestPeersReq
	listPeersReqs    chan PeersOpChannel
	sizeReqs         chan chan int

	updateReqs chan node.RemoteNodeData
	removeReqs chan node.RemoteNodeData

	// latency metrics
	//metrics pstore.Metrics

	// Maximum acceptable latency for peers in this cluster
	//maxLatency time.Duration

	buckets    []Bucket
	bucketsize int // max number of nodes per bucket. typically 10 or 20.

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

		buckets:     []Bucket{NewBucket()},
		bucketsize:  bucketsize,
		local:       localID,
		peerRemoved: make(PeerChannel, 3),
		peerAdded:   make(PeerChannel, 3),

		findReqs:         make(chan PeerByIdRequest, 3),
		nearestPeerReqs:  make(chan PeerByIdRequest, 3),
		nearestPeersReqs: make(chan NearestPeersReq, 3),
		listPeersReqs:    make(chan PeersOpChannel, 3),
		sizeReqs:         make(chan chan int, 3),

		updateReqs: make(chan node.RemoteNodeData, 3),
		removeReqs: make(chan node.RemoteNodeData, 3),

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

func (rt *routingTableImpl) Update(peer node.RemoteNodeData) {
	rt.updateReqs <- peer
}

func (rt *routingTableImpl) Remove(peer node.RemoteNodeData) {
	rt.removeReqs <- peer
}

//// below - non-ts private impl details (should only be called by processEvents() and not directly

// main event processing loop
func (rt *routingTableImpl) processEvents() {
	for {
		select {

		case p := <-rt.updateReqs:
			rt.update(p)

		case p := <-rt.removeReqs:
			rt.remove(p)

		case r := <-rt.sizeReqs:
			tot := 0
			for _, buck := range rt.buckets {
				tot += buck.Len()
			}
			go func() { r <- tot }()

		case r := <-rt.listPeersReqs:

			var peers []node.RemoteNodeData
			for _, buck := range rt.buckets {
				peers = append(peers, buck.Peers()...)
			}
			go func() { r <- &PeersOpResult{Peers: peers} }()

		case r := <-rt.nearestPeersReqs:
			peers := rt.nearestPeers(r.Id, r.Count)
			if r.Callback != nil {
				go func() { r.Callback <- &PeersOpResult{Peers: peers} }()
			}

		case r := <-rt.nearestPeerReqs:

			peers := rt.nearestPeers(r.Id, 1)
			if r.Callback != nil {
				switch len(peers) {
				case 0:
					go func() { r.Callback <- &PeerOpResult{} }()
				default:
					go func() { r.Callback <- &PeerOpResult{peers[0]} }()
				}
			}

		case r := <-rt.findReqs:
			peers := rt.nearestPeers(r.Id, 1)
			if r.Callback != nil {
				if len(peers) == 0 || !peers[0].DhtId().Equals(r.Id) {
					go func() { r.Callback <- &PeerOpResult{} }()
				} else {
					go func() { r.Callback <- &PeerOpResult{peers[0]} }()
				}
			}

		case p := <-rt.peerAdded:

			for _, c := range rt.peerAddedCallbacks {
				go func() { c <- p }()
			}

		case p := <-rt.peerRemoved:

			for _, c := range rt.peerRemovedCallbacks {
				go func() { c <- p }()
			}

		case c := <-rt.registerPeerAddedReq:
			key := getMemoryAddress(c)
			rt.peerAddedCallbacks[key] = c

		case c := <-rt.registerPeerRemovedReq:
			key := getMemoryAddress(c)
			rt.peerRemovedCallbacks[key] = c

		case c := <-rt.unregisterPeerAddedReq:
			key := getMemoryAddress(c)
			delete(rt.peerAddedCallbacks, key)

		case c := <-rt.unregisterPeerRemovedReq:
			key := getMemoryAddress(c)
			delete(rt.peerRemovedCallbacks, key)
		}
	}
}

// String representation of a pointer
func getMemoryAddress(p interface{}) string {
	return fmt.Sprintf("%p", p)
}

// Add or move a peer to the front of its designated bucket
func (rt *routingTableImpl) update(p node.RemoteNodeData) {

	// determine node bucket ide
	cpl := p.DhtId().CommonPrefixLen(rt.local)

	bucketId := cpl
	if bucketId >= len(rt.buckets) {
		// choose last bucket
		bucketId = len(rt.buckets) - 1
	}

	bucket := rt.buckets[bucketId]

	if bucket.Has(p) {
		// Move this node to the front as it is the most recently active
		// Active nodes shiuld be in the front of their buckets and least active one at the back
		bucket.MoveToFront(p)
		return
	}

	/*
		if rt.metrics.LatencyEWMA(p) > rt.maxLatency {
			// Connection doesnt meet requirements, skip!
			return
		}*/

	// New peer, add to bucket - we add newly seen nodes to the front of their designated bucket
	bucket.PushFront(p)

	// must do this as go so we don't get blocked when the chan is full
	go func() { rt.peerAdded <- p }()

	if bucket.Len() > rt.bucketsize { // bucket overflows

		if bucketId == len(rt.buckets)-1 { // last bucket

			// We added the node to the last bucket and this bucket is overflowing
			// Add a new bucket and possibly remove least active node from the table
			n := rt.addNewBucket()
			if n != nil { // only notify if a node was removed
				go func() { rt.peerRemoved <- n }()
			}
			return
		} else {
			// This is not the last bucket but it is overflowing - we remove the least active
			// node from it to keep the number of nodes within the bucket size
			n := bucket.PopBack()
			go func() { rt.peerRemoved <- n }()
			return
		}
	}
}

// Remove a node from the routing table.
// This is to be used when we are sure a node has disconnected completely.
func (rt *routingTableImpl) remove(p node.RemoteNodeData) {

	cpl := p.DhtId().CommonPrefixLen(rt.local)
	bucketId := cpl
	if bucketId >= len(rt.buckets) {
		bucketId = len(rt.buckets) - 1
	}

	bucket := rt.buckets[bucketId]
	bucket.Remove(p)

	go func() { rt.peerRemoved <- p }()
}

// Add a new bucket to the table
// Returns a node that was removed from the table in case of an overflow
func (rt *routingTableImpl) addNewBucket() node.RemoteNodeData {

	// last bucket
	lastBucket := rt.buckets[len(rt.buckets)-1]

	// new bucket
	newBucket := lastBucket.Split(len(rt.buckets)-1, rt.local)

	rt.buckets = append(rt.buckets, newBucket)

	if newBucket.Len() > rt.bucketsize {
		// new bucket is overflowing - we need to split it again
		return rt.addNewBucket()
	}

	// If all elements were on left side of the split and last bucket is full
	if lastBucket.Len() > rt.bucketsize {
		// We remove the least active node in the last bucket and return it
		return lastBucket.PopBack()
	}

	// no node was removed
	return nil
}

// NearestPeers returns a list of the 'count' closest peers to the given ID
func (rt *routingTableImpl) nearestPeers(id dht.ID, count int) []node.RemoteNodeData {

	cpl := id.CommonPrefixLen(rt.local)

	// Get bucket at cpl index or last bucket
	if cpl >= len(rt.buckets) {
		cpl = len(rt.buckets) - 1
	}

	bucket := rt.buckets[cpl]

	var peerArr peerSorter
	peerArr = copyPeersFromList(id, peerArr, bucket.List())
	if len(peerArr) < count {
		// In the case of an unusual split, one bucket may be short or empty.
		// if this happens, search both surrounding buckets for nearby peers
		if cpl > 0 {
			plist := rt.buckets[cpl-1].List()
			peerArr = copyPeersFromList(id, peerArr, plist)
		}

		if cpl < len(rt.buckets)-1 {
			plist := rt.buckets[cpl+1].List()
			peerArr = copyPeersFromList(id, peerArr, plist)
		}
	}

	// Sort by distance to local peer
	sort.Sort(peerArr)

	var out []node.RemoteNodeData
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
