package dht

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

const (
	// IDLength is the length of an ID,  dht ids are 32 bytes
	IDLength = 256 // bits
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
	Update(p discNode)                // adds a peer to the table
	Remove(p discNode)                // remove a peer from the table
	Find(req PeerByIDRequest)         // find a specific peer by node.DhtID
	NearestPeer(req PeerByIDRequest)  // nearest peer to a node.DhtID
	NearestPeers(req NearestPeersReq) // ip to n nearest peers to a node.DhtID

	SelectPeers(qty int) []discNode // Get a list of random peers

	Size(callback chan int) // total # of peers in the table

	Print()
}

// exported helper types

// PeerOpResult is used as a result of a method that returns nil or one peer.
type PeerOpResult struct {
	Peer discNode
}

// PeerOpChannel is a channel that accept a peer op result.
type PeerOpChannel chan *PeerOpResult

// PeersOpResult is a result of a method that returns zero or more peers.
type PeersOpResult struct {
	Peers []discNode
}

// PeersOpChannel is a channel of PeersOpResult.
type PeersOpChannel chan *PeersOpResult

// PeerChannel is a channel of RemoteNodeData
type PeerChannel chan discNode

// ChannelOfPeerChannel is a channel of PeerChannels
type ChannelOfPeerChannel chan PeerChannel

// PeerByIDRequest includes one peer id and a callback PeerOpChannel
type PeerByIDRequest struct {
	ID       node.DhtID
	Callback PeerOpChannel
}

type PeerAndCallback struct {
	peer discNode
	ret  chan struct{}
}

// NearestPeersReq includes one peer id, a count param and a callback PeersOpChannel.
type NearestPeersReq struct {
	ID       node.DhtID
	Count    int
	Callback PeersOpChannel
}

type randomPeersReq struct {
	count int
	cb    chan []discNode
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
	log log.Log

	// Local peer ID that holds this routing table
	local node.DhtID

	// Operations mesh channels
	findReqs         chan PeerByIDRequest
	nearestPeerReqs  chan PeerByIDRequest
	nearestPeersReqs chan NearestPeersReq
	listPeersReqs    chan PeersOpChannel
	randomPeersReq   chan *randomPeersReq
	sizeReqs         chan chan int
	printReq         chan struct{}

	updateReqs chan PeerAndCallback
	removeReqs chan discNode

	buckets    []Bucket
	bucketsize int // max number of nodes per bucket. typically 10 or 20.

	peerRemovedCallbacks map[string]PeerChannel
	peerAddedCallbacks   map[string]PeerChannel

	// /remove
}

// NewRoutingTable creates a new routing table with a given bucket=size and local node node.DhtID
func NewRoutingTable(bucketsize int, localID node.DhtID, log log.Log) *routingTableImpl {
	// Create all our buckets.
	buckets := []Bucket{NewBucket()}

	rt := &routingTableImpl{

		buckets:    buckets,
		bucketsize: bucketsize,
		log:        log,
		local:      localID,

		findReqs:         make(chan PeerByIDRequest, 3),
		randomPeersReq:   make(chan *randomPeersReq),
		nearestPeerReqs:  make(chan PeerByIDRequest, 3),
		nearestPeersReqs: make(chan NearestPeersReq, 3),
		sizeReqs:         make(chan chan int, 3),

		updateReqs: make(chan PeerAndCallback),
		removeReqs: make(chan discNode, 3),

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

func (rt *routingTableImpl) Update(peer discNode) {
	cb := make(chan struct{})
	rt.updateReqs <- PeerAndCallback{peer, cb}
	<-cb
}

func (rt *routingTableImpl) Remove(peer discNode) {
	rt.removeReqs <- peer
}

func (rt *routingTableImpl) SelectPeers(qty int) []discNode {
	cb := make(chan []discNode)
	rpq := &randomPeersReq{
		qty,
		cb,
	}
	rt.randomPeersReq <- rpq
	return <-cb
}

//// below - non-ts private impl details (should only be called by processEvents() and not directly

// main event processing loop
func (rt *routingTableImpl) processEvents() {
	for {
		select {

		case p := <-rt.updateReqs:
			rt.update(p.peer, p.ret)

		case p := <-rt.removeReqs:
			rt.remove(p)

		case r := <-rt.sizeReqs:
			rt.size(r)

		case r := <-rt.nearestPeersReqs:
			peers := rt.nearestPeers(r.ID, r.Count)
			if r.Callback != nil {
				go func() { r.Callback <- &PeersOpResult{Peers: peers} }()
			}

		case r := <-rt.nearestPeerReqs:
			rt.onNearestPeerReq(r)

		case r := <-rt.findReqs:
			rt.onFindReq(r)

		case r := <-rt.randomPeersReq:
			r.cb <- rt.randomPeers(r.count)

		case <-rt.printReq:
			rt.onPrintReq()
		}

	}
}

func (rt *routingTableImpl) randomPeers(qty int) []discNode {
	// TODO: WRITE our own random peer chosing (better than that shamelessly taken from eth version)
	r := make(chan int)
	rt.size(r)
	size := <-r

	if size <= 0 {
		return nil
	}
	type nodeSlice []discNode

	var buckets []nodeSlice
	buckets = make([]nodeSlice, 0, len(rt.buckets)*rt.bucketsize)

	for i := 0; i < len(rt.buckets); i++ {
		peers := rt.buckets[i].Peers()
		if len(peers) > 0 {
			buckets = append(buckets, peers)
		}
	}

	// Shuffle the buckets.
	for i := len(buckets) - 1; i > 0; i-- {
		j := crypto.GetRandomUInt32(uint32(len(buckets)))
		buckets[i], buckets[j] = buckets[j], buckets[i]
	}

	bufSize := qty
	if size < qty {
		bufSize = size
	}
	buf := make([]discNode, bufSize)
	// Move head of each bucket into buf, removing buckets that become empty.
	var i, j int
	for ; i < len(buf); i, j = i+1, (j+1)%len(buckets) {
		b := buckets[j]
		buf[i] = b[0]
		buckets[j] = b[1:]

		if len(b) == 1 {
			buckets = append(buckets[:j], buckets[j+1:]...)
		}
		if len(buckets) == 0 {
			break
		}
	}

	return buf
}

func (rt *routingTableImpl) getBucket(p discNode) (int, Bucket) {

	// determine node bucket based on cpl
	cpl := p.DhtID().CommonPrefixLen(rt.local)
	if cpl >= len(rt.buckets) {
		// choose the last bucket
		cpl = len(rt.buckets) - 1
	}

	return cpl, rt.buckets[cpl]
}

// Update updates the routing table with the given contact. it will be added to the routing table if we have space
// or if its better in terms of latency and recent contact than out oldest contact in the right bucket.
// this keeps fresh nodes at the top of the bucket and make sure we won't lose contact with the network and keep most healthy nodes.
func (rt *routingTableImpl) update(p discNode, cb chan struct{}) {
	var added bool
	var newnode bool
	var cpl int

	defer func() {
		rt.log.With().Debug("dht_update", log.Int("bucket", cpl), log.String("node_id", p.String()), log.Bool("new", newnode), log.Bool("added", added))
		close(cb)
	}()

	if rt.local.Equals(p.DhtID()) {
		rt.log.Warning("Ignoring attempt to add local node to the routing table")
		return
	}

	cpl, bucket := rt.getBucket(p)

	if bucket.Has(p) {
		// Move this node to the front as it is the most-recently active node
		// Active nodes should be in the front of their buckets and least-active one at the back
		bucket.MoveToFront(p)
		return
	}

	newnode = true

	if p.Address() == "" {
		rt.log.Error("Updated non-existing peer without an address pubkey: %v", p.PublicKey().String())
		return
	}

	// this is a new node.
	// todo: consider connection metrics

	// bucket overflows ?
	if bucket.Len() >= rt.bucketsize {
		// we split buckets that our id is in their range - the last bucket.
		if cpl == len(rt.buckets)-1 {
			rt.split()
			// after split we check again in which bucket we should add peer.
			// the structure of the table has changed, so let's recheck if the peer now has a dedicated bucket.
			cpl, bucket = rt.getBucket(p)

			if bucket.Len() >= rt.bucketsize {
				// if we still have no space, discard that peer
				//todo : Replacement cache
				return
			}
			// this will go forward and add that peer
		}

		// todo : kademlia says: ping least recently seen node
		// reality - ping must be async so we won't block all table
		// you don't except update to trigger new connections and such
		// a lot of updates - a lot of pings - BAD.
		// take care of removing bad peers from outside the table
	}
	// new node and bucket isn't full
	bucket.PushFront(p)
	metrics.DHTSize.Add(1)
	added = true
}

func (rt *routingTableImpl) split() {
	lastBucket := len(rt.buckets) - 1

	bucket := rt.buckets[lastBucket]
	newBucket := bucket.Split(lastBucket, rt.local)

	rt.buckets = append(rt.buckets, newBucket)
}

// Remove a node from the routing table.
// Callback to peerRemoved will be called if node was in table and was removed
// If node wasn't in the table then remove doesn't have any side effects on the table
func (rt *routingTableImpl) remove(p discNode) {
	_, bucket := rt.getBucket(p)
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
func (rt *routingTableImpl) nearestPeers(id node.DhtID, count int) []discNode {

	cpl := id.CommonPrefixLen(rt.local)

	// Get bucket at cpl index or last bucket
	if cpl >= len(rt.buckets) {
		cpl = len(rt.buckets) - 1
	}

	bucket := rt.buckets[cpl]

	var peerArr []discNode
	peerArr = append(peerArr, bucket.Peers()...)

	// If the closest bucket didn't have enough contacts,
	// go into additional buckets until we have enough or run out of buckets.
	i := 0
	for len(peerArr) < count {
		i++
		if cpl-i < 0 && cpl+i > len(rt.buckets)-1 {
			break
		}
		var toAdd []discNode

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
	sorted := SortByDhtID(peerArr, id)
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
			p := e.Value.(discNode)
			str += fmt.Sprintf("\t\t%s cpl:%v\n", p.Pretty(), rt.local.CommonPrefixLen(p.DhtID()))
		}
	}
	rt.log.Info(str)
}
