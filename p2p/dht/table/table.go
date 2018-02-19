package table

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"sort"
)

// RoutingTable manages routing to peers.
// All uppercase methods visible to externals packages are thread-safe.
// Don't call package-level methods (lower-case) - they are private not thread-safe.
// Design spec: 'Kademlia: A Design Specification' with most-recently active nodes at the front of each bucket and not the back.
// http://xlattice.sourceforge.net/components/protocol/kademlia/specs.html
type RoutingTable interface {

	// table ops
	Update(p node.RemoteNodeData)      // adds a peer to the table
	Remove(p node.RemoteNodeData)      // remove a peer from the table
	Find(req PeerByIDRequest)          // find a specific peer by dht.ID
	NearestPeer(req PeerByIDRequest)   // nearest peer to a dht.ID
	NearestPeers(req NearestPeersReq)  // ip to n nearest peers to a dht.ID
	ListPeers(callback PeersOpChannel) // list all peers
	Size(callback chan int)            // total # of peers in the table

	// add/remove peers callbacks management - thread safe
	RegisterPeerRemovedCallback(c PeerChannel)   // get called when a  peer is removed
	RegisterPeerAddedCallback(c PeerChannel)     // get called when a peer is added
	UnregisterPeerRemovedCallback(c PeerChannel) // remove addition reg
	UnregisterPeerAddedCallback(c PeerChannel)   // remove removal reg

	Print()
}

// exported helper types

// PeerOpResult is used as a result of a method that returns nil or one peer.
type PeerOpResult struct {
	Peer node.RemoteNodeData
}

// PeerOpChannel is a channel that accept a peer op result.
type PeerOpChannel chan *PeerOpResult

// PeersOpResult is a result of a method that returns zero or more peers.
type PeersOpResult struct {
	Peers []node.RemoteNodeData
}

// PeersOpChannel is a channel of PeersOpResult.
type PeersOpChannel chan *PeersOpResult

// PeerChannel is a channel of RemoteNodeData
type PeerChannel chan node.RemoteNodeData

// ChannelOfPeerChannel is a channel of PeerChannels
type ChannelOfPeerChannel chan PeerChannel

// PeerByIDRequest includes one peer id and a callback PeerOpChannel
type PeerByIDRequest struct {
	ID       dht.ID
	Callback PeerOpChannel
}

// NearestPeersReq includes one peer id, a count param and a callback PeersOpChannel.
type NearestPeersReq struct {
	ID       dht.ID
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
// cpl(l,n1) = 1 -> n1 => bucket[1]
// cpl(l,n2) = 3 -> n2 => bucket[3]
// Closer nodes will appear in buckets with a higher index
// Most recently-seen nodes appear in the top of their buckets while least-often seen nodes at the bottom
type routingTableImpl struct {

	// local peer ID
	local dht.ID

	// ops impls
	findReqs         chan PeerByIDRequest
	nearestPeerReqs  chan PeerByIDRequest
	nearestPeersReqs chan NearestPeersReq
	listPeersReqs    chan PeersOpChannel
	sizeReqs         chan chan int
	printReq         chan bool

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

// NewRoutingTable creates a new routing table with a given bucket=size and local node dht.ID
func NewRoutingTable(bucketsize int, localID dht.ID) RoutingTable {
	rt := &routingTableImpl{

		buckets:     []Bucket{NewBucket()},
		bucketsize:  bucketsize,
		local:       localID,
		peerRemoved: make(PeerChannel, 3),
		peerAdded:   make(PeerChannel, 3),

		findReqs:         make(chan PeerByIDRequest, 3),
		nearestPeerReqs:  make(chan PeerByIDRequest, 3),
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

		printReq: make(chan bool),
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

// Finds a specific peer by ID/ Returns nil in the callback when not found
func (rt *routingTableImpl) Find(req PeerByIDRequest) {
	rt.findReqs <- req
}

// NearestPeer returns a single peer that is nearest to the given ID
func (rt *routingTableImpl) NearestPeer(req PeerByIDRequest) {
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
			peers := rt.nearestPeers(r.ID, r.Count)
			if r.Callback != nil {
				go func() { r.Callback <- &PeersOpResult{Peers: peers} }()
			}

		case r := <-rt.nearestPeerReqs:
			rt.onNearestPeerReq(r)

		case r := <-rt.findReqs:
			rt.onFindReq(r)

		case p := <-rt.peerAdded:
			for _, c := range rt.peerAddedCallbacks {
				go func(c PeerChannel) { c <- p }(c)
			}

		case p := <-rt.peerRemoved:
			for _, c := range rt.peerRemovedCallbacks {
				go func(c PeerChannel) { c <- p }(c)
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

		case <-rt.printReq:
			rt.onPrintReq()
		}
	}
}

// A string representation of a go pointer - Used to create string keys of runtime objects
func getMemoryAddress(p interface{}) string {
	return fmt.Sprintf("%p", p)
}

// Adds or move a node to the front of its designated k-bucket
func (rt *routingTableImpl) update(p node.RemoteNodeData) {

	if rt.local.Equals(p.DhtID()) {
		log.Warning("Ignoring attempt to add local node to the routing table")
		return
	}

	// determine node bucket based on cpl
	cpl := p.DhtID().CommonPrefixLen(rt.local)

	id := cpl
	if id >= len(rt.buckets) {
		// choose the last bucket
		id = len(rt.buckets) - 1
	}

	bucket := rt.buckets[id]

	if bucket.Has(p) {
		// Move this node to the front as it is the most-recently active node
		// Active nodes should be in the front of their buckets and least-active one at the back
		bucket.MoveToFront(p)
		return
	}

	// todo: consider connection metrics

	// New peer, add to bucket - we add newly seen nodes to the front of their designated bucket
	bucket.PushFront(p)

	// must do this as go so we don't get blocked when the chan is full
	go func() { rt.peerAdded <- p }()

	if bucket.Len() > rt.bucketsize { // bucket overflows
		if id == len(rt.buckets)-1 { // last bucket

			// We added the node to the last bucket and this bucket is over-flowing
			// Add a new bucket and possibly remove least-active node from the table
			n := rt.addNewBucket()
			if n != nil { // only notify if a node was removed
				go func() { rt.peerRemoved <- n }()
			}
			return
		}

		// This is not the last bucket but it is overflowing
		// We remove the least active node from it to keep the number of nodes within the bucket size
		n := bucket.PopBack()
		go func() { rt.peerRemoved <- n }()
	}
}

// Remove a node from the routing table.
// Callback to peerRemoved will be called if node was in table and was removed
// If node wasn't in the table then remove doesn't have any side effects on the table
func (rt *routingTableImpl) remove(p node.RemoteNodeData) {

	cpl := p.DhtID().CommonPrefixLen(rt.local)
	bucketID := cpl
	if bucketID >= len(rt.buckets) {
		bucketID = len(rt.buckets) - 1
	}

	bucket := rt.buckets[bucketID]
	removed := bucket.Remove(p)

	if removed {
		go func() { rt.peerRemoved <- p }()
	}
}

// Adds a new bucket to the table
// Returns a node that was removed from the table in case of an overflow, nil otherwise
func (rt *routingTableImpl) addNewBucket() node.RemoteNodeData {

	// the last bucket
	lastBucket := rt.buckets[len(rt.buckets)-1]

	// the new bucket
	newBucket := lastBucket.Split(len(rt.buckets)-1, rt.local)

	rt.buckets = append(rt.buckets, newBucket)

	if newBucket.Len() > rt.bucketsize {
		// new bucket is overflowing - we need to split it again
		return rt.addNewBucket()
	}

	if lastBucket.Len() > rt.bucketsize {
		// If all elements were on left side of the split and last bucket is full
		// We remove the least active node in the last bucket and return it
		return lastBucket.PopBack()
	}

	// no node was removed
	return nil
}

// Internal find peer request handler
func (rt *routingTableImpl) onFindReq(r PeerByIDRequest) {

	peers := rt.nearestPeers(r.ID, 1)
	if r.Callback == nil {
		return
	}

	if len(peers) == 0 || !peers[0].DhtID().Equals(r.ID) {
		log.Info("Did not find %s in the routing table", r.ID.Pretty())
		go func() { r.Callback <- &PeerOpResult{} }()
	} else {
		p := peers[0]
		log.Info("Found %s in the routing table", p.Pretty())
		go func() { r.Callback <- &PeerOpResult{peers[0]} }()
	}
}

func (rt *routingTableImpl) onNearestPeerReq(r PeerByIDRequest) {
	peers := rt.nearestPeers(r.ID, 1)
	if r.Callback != nil {
		switch len(peers) {
		case 0:
			go func() { r.Callback <- &PeerOpResult{} }()
		default:
			go func() { r.Callback <- &PeerOpResult{peers[0]} }()
		}
	}
}

// NearestPeers returns a list of up to count closest peers to the given ID
// Result is sorted by distance from id
func (rt *routingTableImpl) nearestPeers(id dht.ID, count int) []node.RemoteNodeData {

	cpl := id.CommonPrefixLen(rt.local)

	// Get bucket at cpl index or last bucket
	if cpl >= len(rt.buckets) {
		cpl = len(rt.buckets) - 1
	}

	bucket := rt.buckets[cpl]

	var peerArr node.PeerSorter
	peerArr = node.CopyPeersFromList(id, peerArr, bucket.List())

	// todo: this MUST continue until count are returned even if we need to go to additional buckets

	if len(peerArr) < count {
		// In the case of an unusual split, one bucket may be short or empty.
		// Search both surrounding buckets for nearby peers
		if cpl > 0 {
			plist := rt.buckets[cpl-1].List()
			peerArr = node.CopyPeersFromList(id, peerArr, plist)
		}

		if cpl < len(rt.buckets)-1 {
			plist := rt.buckets[cpl+1].List()
			peerArr = node.CopyPeersFromList(id, peerArr, plist)
		}
	}

	// Sort by distance from id
	sort.Sort(peerArr)

	// return up to count nearest nodes
	var out []node.RemoteNodeData
	for i := 0; i < count && i < peerArr.Len(); i++ {
		out = append(out, peerArr[i].Node)
	}

	return out
}

// Print a descriptive statement about the provided RoutingTable
// Only call from external clients not from internal event handlers
func (rt *routingTableImpl) Print() {
	rt.printReq <- true
}

// should only be called form internal event handlers to print table contents
func (rt *routingTableImpl) onPrintReq() {
	log.Info("Routing Table, bs = %d, buckets;", rt.bucketsize, len(rt.buckets))
	for i, b := range rt.buckets {
		log.Info("\tBucket: %d. Items: %d\n", i, b.List().Len())
		for e := b.List().Front(); e != nil; e = e.Next() {
			p := e.Value.(node.RemoteNodeData)
			log.Info("\t\t%s\n", p.Pretty())
		}
	}
}
