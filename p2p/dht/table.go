package dht

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"gopkg.in/op/go-logging.v1"
)

const (
	// IDLength is the length of an ID,  dht ids are 32 bytes
	IDLength = 256
	// BucketCount is the amount of buckets we hold
	// Kademlia says we need a bucket to each bit but reality says half of network would be at bucket 0
	// half of remaining nodes will be in bucket 1 and so forth.
	// that means that most of te buckets won't be even populated most of the time
	// todo : optimize this?
	BucketCount = IDLength / 10
)

// TODO: check the performance of a mutex based routing table

// RoutingTable manages routing to peers.
// All uppercase methods visible to externals packages are thread-safe.
// Don't call package-level methods (lower-case) - they are private not thread-safe.
// Design spec: 'Kademlia: A Design Specification' with most-recently active nodes at the front of each bucket and not the back.
// http://xlattice.sourceforge.net/components/protocol/kademlia/specs.html
type RoutingTable interface {

	// table ops
	Update(p node.Node)                // adds a peer to the table
	Remove(p node.Node)                // remove a peer from the table
	Find(req PeerByIDRequest)          // find a specific peer by identity.DhtID
	NearestPeer(req PeerByIDRequest)   // nearest peer to a identity.DhtID
	NearestPeers(req NearestPeersReq)  // ip to n nearest peers to a identity.DhtID
	ListPeers(callback PeersOpChannel) // list all peers
	Size(callback chan int)            // total # of peers in the table

	Print()
}

// exported helper types

// PeerOpResult is used as a result of a method that returns nil or one peer.
type PeerOpResult struct {
	Peer node.Node
}

// PeerOpChannel is a channel that accept a peer op result.
type PeerOpChannel chan *PeerOpResult

// PeersOpResult is a result of a method that returns zero or more peers.
type PeersOpResult struct {
	Peers []node.Node
}

// PeersOpChannel is a channel of PeersOpResult.
type PeersOpChannel chan *PeersOpResult

// PeerChannel is a channel of RemoteNodeData
type PeerChannel chan node.Node

// ChannelOfPeerChannel is a channel of PeerChannels
type ChannelOfPeerChannel chan PeerChannel

// PeerByIDRequest includes one peer id and a callback PeerOpChannel
type PeerByIDRequest struct {
	ID       node.DhtID
	Callback PeerOpChannel
}

// NearestPeersReq includes one peer id, a count param and a callback PeersOpChannel.
type NearestPeersReq struct {
	ID       node.DhtID
	Count    int
	Callback PeersOpChannel
}

// RoutingTable defines the routing table.
// Most recently network active nodes are placed in beginning of buckets.
// Least active nodes are the back of each bucket.
// Bucket index is the size of the common prefix of nodes in that buckets and the local identity
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
	//logger for this routing table usually the identity logger
	log *logging.Logger

	// Local peer ID that holds this routing table
	local node.DhtID

	// Operations activation channels
	findReqs         chan PeerByIDRequest
	nearestPeerReqs  chan PeerByIDRequest
	nearestPeersReqs chan NearestPeersReq
	listPeersReqs    chan PeersOpChannel
	sizeReqs         chan chan int
	printReq         chan struct{}

	updateReqs chan node.Node
	removeReqs chan node.Node

	// latency metrics
	//metrics pstore.Metrics

	// Maximum acceptable latency for peers in this cluster
	//maxLatency time.Duration

	buckets    [BucketCount]Bucket
	bucketsize int // max number of nodes per bucket. typically 10 or 20.

	peerRemovedCallbacks map[string]PeerChannel
	peerAddedCallbacks   map[string]PeerChannel

	// /remove
}

// NewRoutingTable creates a new routing table with a given bucket=size and local identity identity.DhtID
func NewRoutingTable(bucketsize int, localID node.DhtID, log *logging.Logger) RoutingTable {

	// Create all our buckets.
	buckets := [BucketCount]Bucket{}
	for i := 0; i < BucketCount; i++ {
		buckets[i] = NewBucket()
	}

	rt := &routingTableImpl{

		buckets:    buckets,
		bucketsize: bucketsize,
		log:        log,
		local:      localID,

		findReqs:         make(chan PeerByIDRequest, 3),
		nearestPeerReqs:  make(chan PeerByIDRequest, 3),
		nearestPeersReqs: make(chan NearestPeersReq, 3),
		listPeersReqs:    make(chan PeersOpChannel, 3),
		sizeReqs:         make(chan chan int, 3),

		updateReqs: make(chan node.Node),
		removeReqs: make(chan node.Node, 3),

		peerRemovedCallbacks: make(map[string]PeerChannel),
		peerAddedCallbacks:   make(map[string]PeerChannel),

		printReq: make(chan struct{}),
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

func (rt *routingTableImpl) Update(peer node.Node) {
	rt.updateReqs <- peer
}

func (rt *routingTableImpl) Remove(peer node.Node) {
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
			rt.size(r)

		case r := <-rt.listPeersReqs:
			rt.listPeers(r)

		case r := <-rt.nearestPeersReqs:
			peers := rt.nearestPeers(r.ID, r.Count)
			if r.Callback != nil {
				go func() { r.Callback <- &PeersOpResult{Peers: peers} }()
			}

		case r := <-rt.nearestPeerReqs:
			rt.onNearestPeerReq(r)

		case r := <-rt.findReqs:
			rt.onFindReq(r)

		case <-rt.printReq:
			rt.onPrintReq()
		}

	}
}

// Update updates the routing table with the given contact. it will be added to the routing table if we have space
// or if its better in terms of latency and recent contact than out oldest contact in the right bucket.
// this keeps fresh nodes at the top of the bucket and make sure we won't lose contact with the network and keep most healthy nodes.
func (rt *routingTableImpl) update(p node.Node) {

	if rt.local.Equals(p.DhtID()) {
		rt.log.Warning("Ignoring attempt to add local identity to the routing table")
		return
	}

	// determine identity bucket based on cpl
	cpl := p.DhtID().CommonPrefixLen(rt.local)
	if cpl >= len(rt.buckets) {
		// choose the last bucket
		cpl = len(rt.buckets) - 1
	}

	bucket := rt.buckets[cpl]

	if bucket.Has(p) {
		// Move this identity to the front as it is the most-recently active identity
		// Active nodes should be in the front of their buckets and least-active one at the back
		bucket.MoveToFront(p)
		return
	}

	// todo: consider connection metrics
	if bucket.Len() >= rt.bucketsize { // bucket overflows
		// TODO: if bucket is full ping oldest identity and replace if it fails to answer
		// TODO: check latency metrics and replace if new identity is better then oldest one.
		// Fresh, recent contacted (alive), low latency nodes should be kept at top of the bucket.
		return
	}

	bucket.PushFront(p)
}

// Remove a identity from the routing table.
// Callback to peerRemoved will be called if identity was in table and was removed
// If identity wasn't in the table then remove doesn't have any side effects on the table
func (rt *routingTableImpl) remove(p node.Node) {

	cpl := p.DhtID().CommonPrefixLen(rt.local)
	bucketID := cpl
	if bucketID >= len(rt.buckets) {
		bucketID = len(rt.buckets) - 1
	}

	bucket := rt.buckets[bucketID]
	bucket.Remove(p)
}

// Internal find peer request handler
func (rt *routingTableImpl) onFindReq(r PeerByIDRequest) {

	peers := rt.nearestPeers(r.ID, 1)
	if r.Callback == nil {
		return
	}

	if len(peers) == 0 || !peers[0].DhtID().Equals(r.ID) {
		rt.log.Debug("Did not find %s in the routing table", r.ID.Pretty())
		go func() { r.Callback <- &PeerOpResult{} }()
	} else {
		p := peers[0]
		rt.log.Debug("Found %s in the routing table", p.Pretty())
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
func (rt *routingTableImpl) nearestPeers(id node.DhtID, count int) []node.Node {

	cpl := id.CommonPrefixLen(rt.local)

	// Get bucket at cpl index or last bucket
	if cpl >= len(rt.buckets) {
		cpl = len(rt.buckets) - 1
	}

	bucket := rt.buckets[cpl]

	//var peerArr identity.PeerSorter
	//peerArr = identity.CopyPeersFromList(id, peerArr, bucket.List())
	var peerArr []node.Node
	peerArr = append(peerArr, bucket.Peers()...)

	// If the closest bucket didn't have enough contacts,
	// go into additional buckets until we have enough or run out of buckets.
	i := 0
	for len(peerArr) < count {
		i++
		if cpl-i < 0 && cpl+i > len(rt.buckets)-1 {
			break
		}
		var toAdd []node.Node

		if cpl-i >= 0 {
			plist := rt.buckets[cpl-i]
			toAdd = append(toAdd, plist.Peers()...)

		}

		if cpl+i <= len(rt.buckets)-1 {
			plist := rt.buckets[cpl+i]
			toAdd = append(toAdd, plist.Peers()...)
		}

		peerArr = append(peerArr, toAdd...)
	}

	// Sort by distance from id
	sorted := node.SortByDhtID(peerArr, id)
	// return up to count nearest nodes
	if len(sorted) > count {
		sorted = sorted[:count]
	}
	return sorted
}

func (rt *routingTableImpl) size(callback chan int) {
	tot := 0
	for _, buck := range rt.buckets {
		tot += buck.Len()
	}
	go func() { callback <- tot }()
}

func (rt *routingTableImpl) listPeers(callback PeersOpChannel) {
	var peers []node.Node
	for _, buck := range rt.buckets {
		peers = append(peers, buck.Peers()...)
	}
	go func() { callback <- &PeersOpResult{Peers: peers} }()
}

// Print a descriptive statement about the provided RoutingTable
// Only call from external clients not from internal event handlers
func (rt *routingTableImpl) Print() {
	rt.printReq <- struct{}{}
}

// should only be called form internal event handlers to print table contents
func (rt *routingTableImpl) onPrintReq() {
	str := fmt.Sprintf("%v's Routing Table, bs = %d, buckets: %d", rt.local.Pretty(), rt.bucketsize, len(rt.buckets))
	str += "\n\r"
	for i, b := range rt.buckets {
		str += fmt.Sprintf("\tBucket: %d. Items: %d\n", i, b.List().Len())
		for e := b.List().Front(); e != nil; e = e.Next() {
			p := e.Value.(node.Node)
			str += fmt.Sprintf("\t\t%s cpl:%v\n", p.Pretty(), rt.local.CommonPrefixLen(p.DhtID()))
		}
	}
	rt.log.Debug(str)
}
