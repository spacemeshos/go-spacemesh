package addressbook

import (
	crand "crypto/rand"
	"encoding/binary"
	"math/rand"
	"net"
	"path/filepath"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
)

// concurrentRand provides a concurrency safe random number generator, required
// because sources created by math/rand.NewSource are not safe for concurrent
// use.
type concurrentRand struct {
	r  *rand.Rand
	mu sync.Mutex
}

func newConcurrentRand() *concurrentRand {
	return &concurrentRand{
		r: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (cr *concurrentRand) Intn(n int) int {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	return cr.r.Intn(n)
}

// AddrBook provides a concurrency safe address manager for caching potential
// peers on the network. based on bitcoin's AddrBook.
type AddrBook struct {
	cfg    *Config
	logger log.Log
	path   string

	mu              sync.RWMutex
	rand            *concurrentRand
	key             [32]byte
	addrIndex       map[peer.ID]*knownAddress
	addrNew         []map[peer.ID]*knownAddress
	addrTried       []map[peer.ID]*knownAddress
	anchorPeers     []*knownAddress // Anchor peers. store active connections, and use some of them when node starts.
	lastAnchorPeers []*knownAddress // Anchor peers from last node start. Used only for bootstrap. New connections come to anchorPeers

	// Cache of recently removed peers.
	recentlyRemoved *lru.Cache

	nTried int
	nNew   int
}

// NewAddrBook returns a new address manager.
// Use Start to begin processing asynchronous address updates.
func NewAddrBook(cfg *Config, logger log.Log) *AddrBook {
	path := ""
	if len(cfg.DataDir) != 0 {
		path = filepath.Join(cfg.DataDir, peersFileName)
	}
	cache, err := lru.New(cfg.RemovedPeersCacheSize)
	if err != nil {
		log.With().Panic("Failed to create removed peers cache", log.Err(err), log.Int("size", cfg.RemovedPeersCacheSize))
	}
	am := AddrBook{
		cfg:             cfg,
		logger:          logger,
		path:            path,
		rand:            newConcurrentRand(),
		recentlyRemoved: cache,
	}
	am.reset()
	am.loadPeers(am.path)
	return &am
}

// WasRecentlyRemoved checks if peer ID was recently removed.
// If peer is in the cache, then it returns the time when peer was removed.
// If the peer was not in the cache, returns (nil, false).
func (a *AddrBook) WasRecentlyRemoved(id peer.ID) (removedAt *time.Time, wasRemoved bool) {
	if removedAt, ok := a.recentlyRemoved.Peek(id); ok {
		r := removedAt.(time.Time)
		return &r, ok
	}
	return nil, false
}

func (a *AddrBook) removePeer(id peer.ID) {
	delete(a.addrIndex, id)
	a.recentlyRemoved.Add(id, time.Now())
}

// updateAddress is a helper function to either update an address already known
// to the address manager, or to add the address if not already known.
func (a *AddrBook) updateAddress(addr, src *AddrInfo) {
	routableAddr := IsRoutable(addr.IP) || IsDNSAddress(addr.RawAddr)
	if !routableAddr && IsRoutable(src.IP) {
		a.logger.Debug("skipped non routable address received from routable ip",
			log.String("received", addr.IP.String()),
			log.String("from", src.IP.String()),
		)
		return
	}
	ka := a.lookup(addr.ID)
	if ka != nil {
		// Update the last seen time and services.
		// note that to prevent causing excess garbage on getaddr
		// messages the netaddresses in addrmaanger are *immutable*,
		// if we need to change them then we replace the pointer with a
		// new copy so that we don't have to copy every na for getaddr.
		ka.LastSeen = time.Now()

		if ka.Addr.RawAddr != addr.RawAddr {
			a.logger.Debug("Update address",
				log.String("old", ka.Addr.RawAddr),
				log.String("new", addr.RawAddr),
			)
			ka.Addr = addr
		}

		// If already in tried, we have nothing to do here.
		if ka.tried {
			return
		}

		// Already at our max?
		if ka.refs == a.cfg.NewBucketsPerAddress {
			return
		}

		// The more entries we have, the less likely we are to add more.
		// likelihood is 2N.
		// factor := int32(2 * ka.refs)
		// if a.rand.Int31n(factor) != 0 {
		return
		//}
	}
	// Make a copy of the net address to avoid races since it is
	// updated elsewhere in the addrmanager code and would otherwise
	// change the actual netaddress on the peer.
	ka = &knownAddress{Addr: addr, SrcAddr: src, LastSeen: time.Now()}
	a.addrIndex[addr.ID] = ka
	a.nNew++
	// XXX time penalty?

	bucket := a.getNewBucket(addr.IP, src.IP)

	// Already exists?
	if _, ok := a.addrNew[bucket][addr.ID]; ok {
		return
	}

	// Enforce max addresses.
	if len(a.addrNew[bucket]) >= a.cfg.NewBucketSize {
		a.logger.Debug("new bucket is full, expiring old")
		a.expireNew(bucket)
	}

	// Add to new bucket.
	ka.refs++
	a.addrNew[bucket][addr.ID] = ka

	a.logger.Debug("added new address %s for a total of %d addresses", addr.RawAddr, a.nTried+a.nNew)
}

// Lookup searches for an address using a public key. returns *Info.
func (a *AddrBook) Lookup(addr peer.ID) *AddrInfo {
	a.mu.Lock()
	d := a.lookup(addr)
	a.mu.Unlock()
	if d == nil {
		return nil
	}
	return d.Addr
}

func (a *AddrBook) lookup(addr peer.ID) *knownAddress {
	return a.addrIndex[addr]
}

// toTried moves a knownAddress to a tried bucket.
func (a *AddrBook) toTried(ka *knownAddress) {
	// move to tried set, optionally evicting other addresses if neeed.
	if ka.tried {
		return
	}
	// ok, need to move it to tried.
	addr := ka.Addr
	// remove from all new buckets.
	// record one of the buckets in question and call it the `first'
	oldBucket := -1
	for i := range a.addrNew {
		// we check for existence so we can record the first one
		if _, ok := a.addrNew[i][addr.ID]; ok {
			delete(a.addrNew[i], addr.ID)
			ka.refs--
			if oldBucket == -1 {
				oldBucket = i
			}
		}
	}

	if oldBucket == -1 {
		// What? wasn't in a bucket after all.... Panic?
		return
	}

	a.nNew--

	bucket := a.getTriedBucket(addr.IP)

	// Room in this tried bucket?
	if len(a.addrTried[bucket]) < a.cfg.TriedBucketSize {
		ka.tried = true
		a.addrTried[bucket][addr.ID] = ka
		a.nTried++
		return
	}

	// No room, we have to evict something else.

	rmka := a.pickTried(bucket)

	// First bucket it would have been put in.
	newBucket := a.getNewBucket(rmka.Addr.IP, rmka.SrcAddr.IP)

	// If no room in the original bucket, we put it in a bucket we just
	// freed up a space in.
	if len(a.addrNew[newBucket]) >= a.cfg.NewBucketSize {
		newBucket = oldBucket
	}

	// replace with ka in list.
	ka.tried = true

	a.addrTried[bucket][ka.Addr.ID] = ka

	rmka.tried = false
	rmka.refs++

	// We don't touch a.nTried here since the number of tried stays the same
	// but we decemented new above, raise it again since we're putting
	// something back.
	a.nNew++

	rmkey := rmka.Addr.ID
	a.logger.Debug("Replacing %s with %s in tried", rmkey, addr.ID)
	// We made sure there is space here just above.
	a.addrNew[newBucket][rmkey] = rmka
}

// pickTried selects an address from the tried bucket to be evicted.
// We just choose the eldest. Bitcoind selects 4 random entries and throws away
// the oldest of them.
func (a *AddrBook) pickTried(bucket int) *knownAddress {
	var oldest *knownAddress
	for _, ka := range a.addrTried[bucket] {
		if oldest == nil || oldest.LastSeen.After(ka.LastSeen) {
			oldest = ka
		}
	}
	return oldest
}

// numAddresses returns the number of addresses known to the address manager.
func (a *AddrBook) numAddresses() int {
	return a.nTried + a.nNew
}

// NumAddresses returns the number of addresses known to the address manager.
func (a *AddrBook) NumAddresses() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.numAddresses()
}

// getAddressFromBuckets selects an address from given slice of buckets.
func (a *AddrBook) getAddressFromBuckets(buckets []map[peer.ID]*knownAddress) *knownAddress {
	nonEmptyBuckets := make([]int, 0, len(buckets)) // get non-empty buckets ids for reduce extracting time.
	for i := range buckets {
		if len(buckets[i]) > 0 {
			nonEmptyBuckets = append(nonEmptyBuckets, i)
		}
	}
	if len(nonEmptyBuckets) == 0 {
		return nil // no addresses to extract.
	}
	large := 1 << 30
	factor := 1.0
	for {
		// pick a random bucket.
		bucket := nonEmptyBuckets[a.rand.Intn(len(nonEmptyBuckets))]

		// Pick a random entry in it. Structure is map[peer.ID]*knownAddress, so we need loop to gen rand index.
		var ka *knownAddress
		nth := a.rand.Intn(len(buckets[bucket]))
		for _, value := range buckets[bucket] {
			if nth == 0 {
				ka = value
			}
			nth--
		}

		randVal := a.rand.Intn(large)
		if float64(randVal) < (factor * ka.Chance() * float64(large)) {
			return ka
		}
		factor *= 1.2
	}
}

func (a *AddrBook) GetKnownAddressesCache() []knownAddress {
	allAddr := a.GetAddresses()

	numAddresses := len(allAddr) * a.cfg.GetAddrPercent / 100
	if numAddresses > a.cfg.GetAddrMax {
		numAddresses = a.cfg.GetAddrMax
	} else if numAddresses == 0 {
		numAddresses = len(allAddr)
	}

	result := make([]knownAddress, 0, numAddresses)
	a.mu.RLock()
	defer a.mu.RUnlock()
	for i := 0; i < numAddresses; i++ {
		var ka *knownAddress
		// Use a 50% chance for choosing between tried and new table entries.
		if a.nTried > 0 && (a.nNew == 0 || a.rand.Intn(2) == 0) {
			ka = a.getAddressFromBuckets(a.addrTried)
		} else {
			ka = a.getAddressFromBuckets(a.addrNew)
		}
		if ka != nil {
			result = append(result, *ka)
		}
	}
	return result
}

// AddressCache returns the current address cache.  It must be treated as
// read-only (but since it is a copy now, this is not as dangerous).
func (a *AddrBook) AddressCache() []*AddrInfo {
	knownAddresses := a.GetKnownAddressesCache()
	result := make([]*AddrInfo, 0, len(knownAddresses))
	for _, address := range knownAddresses {
		result = append(result, address.Addr)
	}
	return result
}

// BootstrapAddressCache run AddressCache and add Config.AnchorPeersCount addresses from anchor peers.
func (a *AddrBook) BootstrapAddressCache() (addresses []*AddrInfo) {
	addresses = a.AddressCache()
	a.mu.RLock()
	defer a.mu.RUnlock()

	if len(a.lastAnchorPeers) == 0 {
		return util.UniqueSliceStringer(addresses)
	}

	anchorPeers := make([]*knownAddress, len(a.lastAnchorPeers))
	copy(anchorPeers, a.lastAnchorPeers)

	rand.Seed(int64(randomUint32(16384)))
	rand.Shuffle(len(anchorPeers), func(i, j int) {
		anchorPeers[i], anchorPeers[j] = anchorPeers[j], anchorPeers[i]
	})

	for _, ka := range anchorPeers {
		addresses = append(addresses, ka.Addr)
		if len(addresses) >= a.cfg.AnchorPeersCount {
			break
		}
	}
	return util.UniqueSliceStringer(addresses)
}

// GetAddresses returns all the addresses currently found within the manager's address cache.
func (a *AddrBook) GetAddresses() []*AddrInfo {
	a.mu.Lock()
	defer a.mu.Unlock()

	addrIndexLen := len(a.addrIndex)
	if addrIndexLen == 0 {
		return nil
	}

	addrs := make([]*AddrInfo, 0, addrIndexLen)
	for _, v := range a.addrIndex {
		addrs = append(addrs, v.Addr)
	}

	return addrs
}

// GetAddressesNotConnectedSince returns all the addresses which have not
// been successfully connected to since `date`.
func (a *AddrBook) GetAddressesNotConnectedSince(date time.Time) []*AddrInfo {
	a.mu.Lock()
	defer a.mu.Unlock()

	addrIndexLen := len(a.addrIndex)
	if addrIndexLen == 0 {
		return nil
	}

	addrs := make([]*AddrInfo, 0, addrIndexLen)
	for _, v := range a.addrIndex {
		if v.LastSuccess.Before(date) {
			addrs = append(addrs, v.Addr)
		}
	}
	return addrs
}

// expireNew makes space in the new buckets by expiring the really bad entries.
// If no bad entries are available we look at a few and remove the oldest.
func (a *AddrBook) expireNew(bucket int) {
	// First see if there are any entries that are so bad we can just throw
	// them away. otherwise we throw away the oldest entry in the cache.
	// Bitcoind here chooses four random and just throws the oldest of
	// those away, but we keep track of oldest in the initial traversal and
	// use that information instead.
	var oldest *knownAddress
	for k, v := range a.addrNew[bucket] {
		if a.isBad(v) {
			a.logger.Debug("expiring bad address %v", k)
			delete(a.addrNew[bucket], k)
			v.refs--
			if v.refs == 0 {
				a.nNew--
				a.removePeer(k)
			}
			continue
		}
		if oldest == nil {
			oldest = v
		} else if !v.LastSeen.After(oldest.LastSeen) {
			oldest = v
		}
	}

	if oldest != nil {
		key := oldest.Addr.ID
		delete(a.addrNew[bucket], key)
		oldest.refs--
		if oldest.refs == 0 {
			a.nNew--
			a.removePeer(key)
		}
	}
}

func doubleHash(buf []byte) []byte {
	first := hash.Sum(buf)
	second := hash.Sum(first[:])
	return second[:]
}

func (a *AddrBook) getNewBucket(netAddr, srcAddr net.IP) int {
	// bitcoind:
	// doublesha256(key + sourcegroup + int64(doublesha256(key + group + sourcegroup))%bucket_per_source_group) % num_new_buckets

	var data1 []byte
	data1 = append(data1, a.key[:]...)
	data1 = append(data1, []byte(GroupKey(netAddr))...)
	data1 = append(data1, []byte(GroupKey(srcAddr))...)
	hash1 := doubleHash(data1)
	hash64 := binary.LittleEndian.Uint64(hash1)
	hash64 %= a.cfg.NewBucketsPerGroup
	var hashbuf [8]byte
	binary.LittleEndian.PutUint64(hashbuf[:], hash64)
	var data2 []byte
	data2 = append(data2, a.key[:]...)
	data2 = append(data2, GroupKey(srcAddr)...)
	data2 = append(data2, hashbuf[:]...)

	hash2 := doubleHash(data2)
	return int(binary.LittleEndian.Uint64(hash2) % a.cfg.NewBucketCount)
}

func (a *AddrBook) getTriedBucket(netAddr net.IP) int {
	// bitcoind hashes this as:
	// doublesha256(key + group + truncate_to_64bits(doublesha256(key)) % buckets_per_group) % num_buckets
	var data1 []byte
	data1 = append(data1, a.key[:]...)
	data1 = append(data1, []byte(netAddr)...)
	hash1 := doubleHash(data1)
	hash64 := binary.LittleEndian.Uint64(hash1)
	hash64 %= a.cfg.TriedBucketsPerGroup
	var hashbuf [8]byte
	binary.LittleEndian.PutUint64(hashbuf[:], hash64)
	var data2 []byte
	data2 = append(data2, a.key[:]...)
	data2 = append(data2, GroupKey(netAddr)...)
	data2 = append(data2, hashbuf[:]...)

	hash2 := doubleHash(data2)
	return int(binary.LittleEndian.Uint64(hash2) % a.cfg.TriedBucketCount)
}

// AddAddresses adds new addresses to the address manager.  It enforces a max
// number of addresses and silently ignores duplicate addresses.  It is
// safe for concurrent access.
func (a *AddrBook) AddAddresses(addrs []*AddrInfo, src *AddrInfo) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, na := range addrs {
		a.updateAddress(na, src)
	}
}

// AddAddress adds a new address to the address manager.  It enforces a max
// number of addresses and silently ignores duplicate addresses.  It is
// safe for concurrent access.
func (a *AddrBook) AddAddress(addr, src *AddrInfo) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.updateAddress(addr, src)
}

// Attempt increases the given address' attempt counter and updates the last attempt time.
func (a *AddrBook) Attempt(pid peer.ID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ka := a.lookup(pid)
	if ka == nil {
		return
	}

	ka.Attempts++
	ka.LastAttempt = time.Now()
	a.toTried(ka)
}

// Good marks the given address as good.  To be called after a successful
// connection and version exchange.  If the address is unknown to the address
// manager it will be ignored.
func (a *AddrBook) Good(pid peer.ID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ka := a.lookup(pid)
	if ka == nil {
		return
	}

	now := time.Now()
	ka.LastSuccess = now
	ka.LastAttempt = now
	ka.LastSeen = now
	ka.Attempts = 0
	a.toTried(ka)
}

// Connected adds the given address to anchor list. Will take some addresses this when node will start.
func (a *AddrBook) Connected(peerID peer.ID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ka, ok := a.addrIndex[peerID]
	if ok {
		a.anchorPeers = append(a.anchorPeers, ka)
	}
}

// RemoveAddress removes the address from the manager.
func (a *AddrBook) RemoveAddress(pid peer.ID) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.addrIndex[pid] == nil {
		return
	}

	for _, b := range a.addrNew {
		if _, ok := b[pid]; ok {
			delete(b, pid)
			a.nNew--
		}
	}

	for _, b := range a.addrTried {
		if _, ok := b[pid]; ok {
			delete(b, pid)
			a.nTried--
		}
	}

	for i, ka := range a.anchorPeers {
		if ka.Addr.ID == pid {
			a.anchorPeers = append(a.anchorPeers[:i], a.anchorPeers[i+1:]...)
			break
		}
	}

	a.removePeer(pid)
}

// reset resets the address manager by reinitialising the random source and allocating fresh empty bucket storage.
func (a *AddrBook) reset() {
	a.addrIndex = make(map[peer.ID]*knownAddress)
	a.anchorPeers = make([]*knownAddress, 0)
	a.lastAnchorPeers = make([]*knownAddress, 0)
	a.addrNew = make([]map[peer.ID]*knownAddress, a.cfg.NewBucketCount)
	a.addrTried = make([]map[peer.ID]*knownAddress, a.cfg.TriedBucketCount)

	// fill key with bytes from a good random source.
	if _, err := crand.Read(a.key[:]); err != nil {
		a.logger.Panic("Error generating random bytes %v", err)
	}

	for i := range a.addrNew {
		a.addrNew[i] = make(map[peer.ID]*knownAddress)
	}
	for i := range a.addrTried {
		a.addrTried[i] = make(map[peer.ID]*knownAddress)
	}
}

func randomUint32(max uint32) uint32 {
	b := make([]byte, 4)
	_, err := crand.Read(b)
	if err != nil {
		log.Panic("Failed to get entropy from system: ", err)
	}

	data := binary.BigEndian.Uint32(b)
	return data % max
}
