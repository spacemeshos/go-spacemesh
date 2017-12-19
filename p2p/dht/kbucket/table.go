package kbucket

import (
	"fmt"
	"github.com/UnrulyOS/go-unruly/p2p"
	"github.com/UnrulyOS/go-unruly/p2p/dht"
	"sort"
	"sync"
)

type RoutingTable interface {
	Update(p p2p.RemoteNodeData)
	Remove(p p2p.RemoteNodeData)
	nextBucket() p2p.RemoteNodeData
	Find(id dht.ID) p2p.RemoteNodeData
	NearestPeer(id dht.ID) p2p.RemoteNodeData
	NearestPeers(id dht.ID, count int) []p2p.RemoteNodeData
	ListPeers() []p2p.RemoteNodeData
	Size() int

	RegisterPeerRemovedCallback(c peerChannel)
	RegisterPeerAddedCallback(c peerChannel)

	UnregisterPeerRemovedCallback(c peerChannel)
	UnregisterPeerAddedCallback(c peerChannel)
}

type peerChannel chan p2p.RemoteNodeData
type channelOfPeerChannel chan peerChannel

// RoutingTable defines the routing table.
type routingTableImpl struct {

	// ID of the local peer
	local dht.ID

	// Blanket lock, refine later for better performance
	tabLock sync.RWMutex

	// latency metrics
	//metrics pstore.Metrics

	// Maximum acceptable latency for peers in this cluster
	//maxLatency time.Duration

	// kBuckets define all the fingers to other nodes.
	Buckets    []Bucket
	bucketsize int

	peerRemoved peerChannel
	peerAdded   peerChannel

	registerPeerAddedReq     channelOfPeerChannel
	registerPeerRemovedReq   channelOfPeerChannel
	unregisterPeerAddedReq   channelOfPeerChannel
	unregisterPeerRemovedReq channelOfPeerChannel

	peerRemovedCallbacks map[string]peerChannel
	peerAddedCallbacks   map[string]peerChannel
}

// NewRoutingTable creates a new routing table with a given bucketsize, local ID, and latency tolerance.
func NewRoutingTable(bucketsize int, localID dht.ID) RoutingTable {
	rt := &routingTableImpl{
		Buckets:     []Bucket{newBucket()},
		bucketsize:  bucketsize,
		local:       localID,
		peerRemoved: make(peerChannel, 3),
		peerAdded:   make(peerChannel, 3),

		registerPeerAddedReq:     make(channelOfPeerChannel, 3),
		registerPeerRemovedReq:   make(channelOfPeerChannel, 3),
		unregisterPeerAddedReq:   make(channelOfPeerChannel, 3),
		unregisterPeerRemovedReq: make(channelOfPeerChannel, 3),

		peerRemovedCallbacks: make(map[string]peerChannel),
		peerAddedCallbacks:   make(map[string]peerChannel),
	}

	go rt.processEvents()

	return rt
}

func pointerToString(p interface{}) string {
	return fmt.Sprintf("%p", p)
}

func (rt *routingTableImpl) processEvents() {
	for {
		select {
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

func (rt *routingTableImpl) RegisterPeerRemovedCallback(c peerChannel) {
	rt.registerPeerRemovedReq <- c
}

func (rt *routingTableImpl) RegisterPeerAddedCallback(c peerChannel) {
	rt.registerPeerAddedReq <- c
}

func (rt *routingTableImpl) UnregisterPeerRemovedCallback(c peerChannel) {
	rt.unregisterPeerRemovedReq <- c
}

func (rt *routingTableImpl) UnregisterPeerAddedCallback(c peerChannel) {
	rt.unregisterPeerAddedReq <- c
}

// Update adds or moves the given peer to the front of its respective bucket
// If a peer gets removed from a bucket, it is returned
func (rt *routingTableImpl) Update(p p2p.RemoteNodeData) {

	cpl := p.DhtId().CommonPrefixLen(rt.local)

	rt.tabLock.Lock()
	defer rt.tabLock.Unlock()
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
func (rt *routingTableImpl) Remove(p p2p.RemoteNodeData) {

	rt.tabLock.Lock()
	defer rt.tabLock.Unlock()

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

// Find a specific peer by ID or return nil
func (rt *routingTableImpl) Find(id dht.ID) p2p.RemoteNodeData {
	srch := rt.NearestPeers(id, 1)
	if len(srch) == 0 || !srch[0].DhtId().Equals(id) {
		return nil
	}
	return srch[0]
}

// NearestPeer returns a single peer that is nearest to the given ID
func (rt *routingTableImpl) NearestPeer(id dht.ID) p2p.RemoteNodeData {
	peers := rt.NearestPeers(id, 1)
	if len(peers) > 0 {
		return peers[0]
	}

	//log.Debugf("NearestPeer: Returning nil, table size = %d", rt.Size())
	return nil
}

// NearestPeers returns a list of the 'count' closest peers to the given ID
func (rt *routingTableImpl) NearestPeers(id dht.ID, count int) []p2p.RemoteNodeData {

	cpl := id.CommonPrefixLen(rt.local)

	rt.tabLock.RLock()

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
	rt.tabLock.RUnlock()

	// Sort by distance to local peer
	sort.Sort(peerArr)

	var out []p2p.RemoteNodeData
	for i := 0; i < count && i < peerArr.Len(); i++ {
		out = append(out, peerArr[i].node)
	}

	return out
}

// Size returns the total number of peers in the routing table
func (rt *routingTableImpl) Size() int {
	var tot int
	rt.tabLock.RLock()
	for _, buck := range rt.Buckets {
		tot += buck.Len()
	}
	rt.tabLock.RUnlock()
	return tot
}

// ListPeers takes a RoutingTable and returns a list of all peers from all buckets in the table.
func (rt *routingTableImpl) ListPeers() []p2p.RemoteNodeData {
	var peers []p2p.RemoteNodeData
	rt.tabLock.RLock()
	for _, buck := range rt.Buckets {
		peers = append(peers, buck.Peers()...)
	}
	rt.tabLock.RUnlock()
	return peers
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
