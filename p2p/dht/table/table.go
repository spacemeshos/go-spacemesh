package table

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"gopkg.in/op/go-logging.v1"
	"time"
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

	Bootstrap(findnode func(id string, nodes chan node.RemoteNodeData), id string, minPeers int, errchan chan error)
	IsHealthy() bool

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
	//logger for this routing table usually the node logger
	log *logging.Logger

	// Local peer ID that holds this routing table
	local dht.ID

	// Operations activation channels
	findReqs         chan PeerByIDRequest
	nearestPeerReqs  chan PeerByIDRequest
	nearestPeersReqs chan NearestPeersReq
	listPeersReqs    chan PeersOpChannel
	sizeReqs         chan chan int
	printReq         chan struct{}

	updateReqs chan node.RemoteNodeData
	removeReqs chan node.RemoteNodeData

	// latency metrics
	//metrics pstore.Metrics

	// Maximum acceptable latency for peers in this cluster
	//maxLatency time.Duration

	buckets        [BucketCount]Bucket
	bucketsize     int // max number of nodes per bucket. typically 10 or 20.
	minPeersHealth int

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
func NewRoutingTable(bucketsize int, localID dht.ID, log *logging.Logger) RoutingTable {

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

func (rt *routingTableImpl) IsHealthy() bool {

	if rt.minPeersHealth == 0 {
		return false
	}

	size := make(chan int)
	rt.Size(size)
	s := <-size
	return s >= rt.minPeersHealth
}

func (rt *routingTableImpl) Bootstrap(findnode func(id string, nodes chan node.RemoteNodeData), id string, minPeers int, errchan chan error) {
	timeout := time.NewTimer(90 * time.Second)
	bootstrap := func() chan node.RemoteNodeData { c := make(chan node.RemoteNodeData); findnode(id, c); return c }
	rt.minPeersHealth = minPeers
	bs := bootstrap()

BSLOOP:
	for {
		select {
		case res := <-bs:
			if res != nil {
				errchan <- errors.New("found ourselves in a bootstrap query")
				return
			}

			if rt.IsHealthy() {
				break BSLOOP
			}

			rt.log.Warning("Bootstrap didn't fill routing table, starting bootstrap op again in 2 seconds.")
			time.Sleep(2 * time.Second)
			bs = bootstrap()
			continue
		case <-timeout.C:
			errchan <- errors.New("didn't get response in timeout time")
			return
		}
	}

	// Start the bootstrap refresh loop
	go func() {
		bootSignal := time.NewTicker(2 * time.Minute)
		for {
			<-bootSignal.C
			bootstrap()
		}
	}()

	errchan <- nil
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

func (rt *routingTableImpl) PeerAdded(peer node.RemoteNodeData) {
	rt.peerAdded <- peer
}

func (rt *routingTableImpl) PeerRemoved(peer node.RemoteNodeData) {
	rt.peerRemoved <- peer
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

// Update updates the routing table with the given contact. it will be added to the routing table if we have space
// or if its better in terms of latency and recent contact than out oldest contact in the right bucket.
// this keeps fresh nodes at the top of the bucket and make sure we won't lose contact with the network and keep most healthy nodes.
func (rt *routingTableImpl) update(p node.RemoteNodeData) {

	if rt.local.Equals(p.DhtID()) {
		rt.log.Warning("Ignoring attempt to add local node to the routing table")
		return
	}

	// determine node bucket based on cpl
	cpl := p.DhtID().CommonPrefixLen(rt.local)
	if cpl >= len(rt.buckets) {
		// choose the last bucket
		cpl = len(rt.buckets) - 1
	}

	bucket := rt.buckets[cpl]

	if bucket.Has(p) {
		// Move this node to the front as it is the most-recently active node
		// Active nodes should be in the front of their buckets and least-active one at the back
		bucket.MoveToFront(p)
		return
	}

	// todo: consider connection metrics
	if bucket.Len() >= rt.bucketsize { // bucket overflows
		// TODO: if bucket is full ping oldest node and replace if it fails to answer
		// TODO: check latency metrics and replace if new node is better then oldest one.
		// Fresh, recent contacted (alive), low latency nodes should be kept at top of the bucket.
		return
	}

	bucket.PushFront(p)
	go rt.PeerAdded(p)
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
		go rt.PeerRemoved(p)
	}
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
func (rt *routingTableImpl) nearestPeers(id dht.ID, count int) []node.RemoteNodeData {

	cpl := id.CommonPrefixLen(rt.local)

	// Get bucket at cpl index or last bucket
	if cpl >= len(rt.buckets) {
		cpl = len(rt.buckets) - 1
	}

	bucket := rt.buckets[cpl]

	//var peerArr node.PeerSorter
	//peerArr = node.CopyPeersFromList(id, peerArr, bucket.List())
	var peerArr []node.RemoteNodeData
	peerArr = append(peerArr, bucket.Peers()...)

	// If the closest bucket didn't have enough contacts,
	// go into additional buckets until we have enough or run out of buckets.
	i := 0
	for len(peerArr) < count {
		i++
		if cpl-i < 0 && cpl+i > len(rt.buckets)-1 {
			break
		}
		var toAdd []node.RemoteNodeData

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
	sorted := node.SortClosestPeers(peerArr, id)
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
	var peers []node.RemoteNodeData
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
			p := e.Value.(node.RemoteNodeData)
			str += fmt.Sprintf("\t\t%s cpl:%v\n", p.Pretty(), rt.local.CommonPrefixLen(p.DhtID()))
		}
	}
	rt.log.Debug(str)
}
