package discovery

import (
	"encoding/binary"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

const (
	// needAddressThreshold is the number of addresses under which the
	// address manager will claim to need more addresses.
	needAddressThreshold = 1000

	// triedBucketSize is the maximum number of addresses in each
	// tried address bucket.
	triedBucketSize = 256

	// triedBucketCount is the number of buckets we split tried
	// addresses over.
	triedBucketCount = 64

	// newBucketSize is the maximum number of addresses in each new address
	// bucket.
	newBucketSize = 120

	// newBucketCount is the number of buckets that we spread new addresses
	// over.
	newBucketCount = 1024

	// triedBucketsPerGroup is the number of tried buckets over which an
	// address group will be spread.
	triedBucketsPerGroup = 8

	// newBucketsPerGroup is the number of new buckets over which an
	// source address group will be spread.
	newBucketsPerGroup = 64

	// newBucketsPerAddress is the number of buckets a frequently seen new
	// address may end up in.
	newBucketsPerAddress = 8

	// numMissingDays is the number of days before which we assume an
	// address has vanished if we have not seen it announced  in that long.
	numMissingDays = 30

	// numRetries is the number of tried without a single success before
	// we assume an address is bad.
	numRetries = 3

	// maxFailures is the maximum number of failures we will accept without
	// a success before considering an address bad.
	maxFailures = 10

	// minBadDays is the number of days since the last success before we
	// will consider evicting an address.
	minBadDays = 7

	// getAddrMax is the most addresses that we will send in response
	// to a getAddr (in practise the most addresses we will return from a
	// call to AddressCache()).
	getAddrMax = 300

	// getAddrPercent is the percentage of total addresses known that we
	// will share with a call to AddressCache.
	getAddrPercent = 23
)

// addrBook provides a concurrency safe address manager for caching potential
// peers on the bitcoin network.
type addrBook struct {
	logger log.Log
	mtx    sync.Mutex

	rand *rand.Rand
	key  [32]byte
	// todo: consider different lock to index (rw?)
	addrIndex map[node.ID]*KnownAddress // address key to ka for all addrs.
	// todo: use arrays instead of maps
	addrNew   [newBucketCount]map[node.ID]*KnownAddress
	addrTried [triedBucketCount]map[node.ID]*KnownAddress

	// Keep track of how recently we've successfully completed a roundtrip ping with an address.
	addrPinged map[node.ID]*KnownAddress

	//todo: lock local for updates
	localAddress *node.NodeInfo

	nTried int
	nNew   int

	started  int32
	shutdown int32
	wg       sync.WaitGroup

	quit chan struct{}
}

// updateAddress is a helper function to either update an address already known
// to the address manager, or to add the address if not already known.
func (a *addrBook) updateAddress(netAddr, srcAddr *node.NodeInfo) {

	//Filter out non-routable addresses. Note that non-routable
	//also includes invalid and local addresses.
	if !IsRoutable(netAddr.IP) && IsRoutable(srcAddr.IP) {
		// XXX: this makes tests work with unroutable addresses(loopback)
		return
	}

	ka := a.find(netAddr.PublicKey())
	if ka != nil {
		// TODO: only update addresses periodically.
		// Update the last seen time and services.
		// note that to prevent causing excess garbage on getaddr
		// messages the netaddresses in addrmaanger are *immutable*,
		// if we need to change them then we replace the pointer with a
		// new copy so that we don't have to copy every na for getaddr.
		ka.lastSeen = time.Now()

		// If already in tried, we have nothing to do here.
		if ka.tried {
			return
		}

		// Already at our max?
		if ka.refs == newBucketsPerAddress {
			return
		}

		// The more entries we have, the less likely we are to add more.
		// likelihood is 2N.
		//factor := int32(2 * ka.refs)
		//if a.rand.Int31n(factor) != 0 {
		return
		//}
	} else {
		// Make a copy of the net address to avoid races since it is
		// updated elsewhere in the addrmanager code and would otherwise
		// change the actual netaddress on the peer.
		ka = &KnownAddress{na: netAddr, srcAddr: srcAddr, lastSeen: time.Now()}
		a.addrIndex[netAddr.ID] = ka
		a.nNew++
		// XXX time penalty?
	}

	bucket := a.getNewBucket(netAddr.IP, srcAddr.IP)

	// Already exists?
	if _, ok := a.addrNew[bucket][netAddr.ID]; ok {
		return
	}

	// Enforce max addresses.
	if len(a.addrNew[bucket]) > newBucketSize {
		a.logger.Debug("new bucket is full, expiring old")
		a.expireNew(bucket)
	}

	// Add to new bucket.
	ka.refs++
	a.addrNew[bucket][netAddr.ID] = ka

	a.logger.Debug("Added new address %s for a total of %d addresses", netAddr.String(), a.nTried+a.nNew)
}

// GetAddress returns a single address that should be routable.  It picks a
// random one from the possible addresses with preference given to ones that
// have not been used recently and should not pick 'close' addresses
// consecutively.
func (a *addrBook) GetAddress() *KnownAddress {
	// Protect concurrent access.
	a.mtx.Lock()
	defer a.mtx.Unlock()

	if a.numAddresses() == 0 {
		return nil
	}

	// Use a 50% chance for choosing between tried and new table entries.
	if a.nTried > 0 && (a.nNew == 0 || a.rand.Intn(2) == 0) {
		// Tried entry.
		large := 1 << 30
		factor := 1.0
		for {
			// pick a random bucket.
			bucket := a.rand.Intn(len(a.addrTried))
			if len(a.addrTried[bucket]) == 0 {
				continue
			}

			// Pick a random entry in it
			var ka *KnownAddress
			nth := a.rand.Intn(len(a.addrTried[bucket]))
			for _, value := range a.addrTried[bucket] {
				if nth == 0 {
					ka = value
				}
				nth--
			}

			randval := a.rand.Intn(large)
			if float64(randval) < (factor * ka.chance() * float64(large)) {
				a.logger.Debug("Selected %v from tried bucket", ka.na.String())
				return ka
			}
			factor *= 1.2
		}
	} else {
		// new node.
		// XXX use a closure/function to avoid repeating this.
		large := 1 << 30
		factor := 1.0
		for {
			// Pick a random bucket.
			bucket := a.rand.Intn(len(a.addrNew))
			if len(a.addrNew[bucket]) == 0 {
				continue
			}
			// Then, a random entry in it.
			var ka *KnownAddress
			nth := a.rand.Intn(len(a.addrNew[bucket]))
			for _, value := range a.addrNew[bucket] {
				if nth == 0 {
					ka = value
				}
				nth--
			}
			randval := a.rand.Intn(large)
			if float64(randval) < (factor * ka.chance() * float64(large)) {
				a.logger.Debug("Selected %v from new bucket", ka.na.String())
				return ka
			}
			factor *= 1.2
		}
	}
}

func (a *addrBook) Lookup(addr p2pcrypto.PublicKey) (*node.NodeInfo, error) {
	a.mtx.Lock()
	d, ok := a.addrIndex[addr.Array()]
	a.mtx.Unlock()
	if !ok {
		// Todo: just return empty without error ?
		return nil, ErrLookupFailed
	}
	return d.na, nil
}

func (a *addrBook) find(addr p2pcrypto.PublicKey) *KnownAddress {
	return a.addrIndex[addr.Array()]
}

// Attempt increases the given address' attempt counter and updates
// the last attempt time.
func (a *addrBook) Attempt(key p2pcrypto.PublicKey) {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	// find address.
	// Surely address will be in tried by now?
	ka := a.find(key)
	if ka == nil {
		return
	}
	// set last tried time to now
	ka.attempts++
	ka.lastattempt = time.Now()
}

// Good marks the given address as good.  To be called after a successful
// connection and version exchange.  If the address is unknown to the address
// manager it will be ignored.
func (a *addrBook) Good(addr p2pcrypto.PublicKey) {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	ka := a.find(addr)
	if ka == nil {
		return
	}

	now := time.Now()
	ka.lastsuccess = now
	ka.lastattempt = now
	ka.lastSeen = now
	ka.attempts = 0

	// move to tried set, optionally evicting other addresses if neeed.
	if ka.tried {
		return
	}

	// ok, need to move it to tried.

	// remove from all new buckets.
	// record one of the buckets in question and call it the `first'
	addrKey := addr.Array()
	oldBucket := -1
	for i := range a.addrNew {
		// we check for existence so we can record the first one
		if _, ok := a.addrNew[i][addrKey]; ok {
			delete(a.addrNew[i], addrKey)
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

	bucket := a.getTriedBucket(ka.na.IP)

	// Room in this tried bucket?
	if len(a.addrTried[bucket]) < triedBucketSize {
		ka.tried = true
		a.addrTried[bucket][addr.Array()] = ka
		a.nTried++
		return
	}

	// No room, we have to evict something else.

	rmka := a.pickTried(bucket)

	// First bucket it would have been put in.
	newBucket := a.getNewBucket(rmka.na.IP, rmka.srcAddr.IP)

	// If no room in the original bucket, we put it in a bucket we just
	// freed up a space in.
	if len(a.addrNew[newBucket]) >= newBucketSize {
		newBucket = oldBucket
	}

	// replace with ka in list.
	ka.tried = true

	a.addrTried[bucket][ka.na.ID] = ka

	rmka.tried = false
	rmka.refs++

	// We don't touch a.nTried here since the number of tried stays the same
	// but we decemented new above, raise it again since we're putting
	// something back.
	a.nNew++

	rmkey := rmka.na.ID
	a.logger.Debug("Replacing %s with %s in tried", rmkey, addrKey)
	// We made sure there is space here just above.
	a.addrNew[newBucket][rmkey] = rmka
}

// pickTried selects an address from the tried bucket to be evicted.
// We just choose the eldest. Bitcoind selects 4 random entries and throws away
// the older of them.
func (a *addrBook) pickTried(bucket int) *KnownAddress {
	var oldest *KnownAddress
	for _, ka := range a.addrTried[bucket] {
		if oldest == nil || oldest.lastSeen.After(ka.lastSeen) {
			oldest = ka
		}

	}
	return oldest
}

// NumAddresses returns the number of addresses known to the address manager.
func (a *addrBook) numAddresses() int {
	return a.nTried + a.nNew
}

// NumAddresses returns the number of addresses known to the address manager.
func (a *addrBook) NumAddresses() int {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	return a.numAddresses()
}

// NeedNewAddresses returns whether or not the address manager needs more new
// addresses. this means we have less new addresses than tried addresses and we don't
// have more than half of the threshold
func (a *addrBook) NeedNewAddresses() bool {
	a.mtx.Lock()
	if a.nNew < a.nTried && a.nNew < needAddressThreshold/2 {
		a.mtx.Unlock()
		return true
	}
	a.mtx.Unlock()

	return false
}

// AddressCache returns the current address cache.  It must be treated as
// read-only (but since it is a copy now, this is not as dangerous).
func (a *addrBook) AddressCache() []*node.NodeInfo {

	// TODO : take from buckets

	allAddr := a.getAddresses()

	numAddresses := len(allAddr) * getAddrPercent / 100
	if numAddresses > getAddrMax {
		numAddresses = getAddrMax
	} else if numAddresses == 0 {
		numAddresses = len(allAddr)
	}

	// Fisher-Yates shuffle the array. We only need to do the first
	// `numAddresses' since we are throwing the rest.
	for i := 0; i < numAddresses; i++ {
		// pick a number between current index and the end
		j := rand.Intn(len(allAddr)-i) + i
		allAddr[i], allAddr[j] = allAddr[j], allAddr[i]
	}

	// slice off the limit we are willing to share.
	return allAddr[0:numAddresses]
}

// getAddresses returns all of the addresses currently found within the
// manager's address cache.
func (a *addrBook) getAddresses() []*node.NodeInfo {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	addrIndexLen := len(a.addrIndex)
	if addrIndexLen == 0 {
		return nil
	}

	addrs := make([]*node.NodeInfo, 0, addrIndexLen)
	for _, v := range a.addrIndex {
		addrs = append(addrs, v.na)
	}

	return addrs
}

// Start begins the core address handler which manages a pool of known
// addresses, timeouts, and interval based writes.
func (a *addrBook) Start() {
	// Already started?
	if atomic.AddInt32(&a.started, 1) != 1 {
		return
	}
	// todo: load peers from hard drive
	// todo: save peers to hard drive
}

// Stop gracefully shuts down the address manager by stopping the main handler.
func (a *addrBook) Stop() {
	if atomic.AddInt32(&a.shutdown, 1) != 1 {
		a.logger.Warning("Address manager is already in the process of " +
			"shutting down")
		return
	}

	a.logger.Info("Address manager shutting down")
	close(a.quit)
	a.wg.Wait()
	return
}

// expireNew makes space in the new buckets by expiring the really bad entries.
// If no bad entries are available we look at a few and remove the oldest.
func (a *addrBook) expireNew(bucket int) {
	// First see if there are any entries that are so bad we can just throw
	// them away. otherwise we throw away the oldest entry in the cache.
	// Bitcoind here chooses four random and just throws the oldest of
	// those away, but we keep track of oldest in the initial traversal and
	// use that information instead.
	var oldest *KnownAddress
	for k, v := range a.addrNew[bucket] {
		if v.isBad() {
			a.logger.Debug("expiring bad address %v", k)
			delete(a.addrNew[bucket], k)
			v.refs--
			if v.refs == 0 {
				a.nNew--
				delete(a.addrIndex, k)
			}
			continue
		}
		if oldest == nil {
			oldest = v
		} else if !v.lastSeen.After(oldest.lastSeen) {
			oldest = v
		}
	}

	if oldest != nil {
		key := oldest.na.ID
		delete(a.addrNew[bucket], key)
		oldest.refs--
		if oldest.refs == 0 {
			a.nNew--
			delete(a.addrIndex, key)
		}
	}
}

// pickTried selects an address from the tried bucket to be evicted.
// We just choose the eldest. Bitcoind selects 4 random entries and throws away
// the older of them.
func (a *addrBook) oldestTried(bucket int) *KnownAddress {
	var oldest *KnownAddress
	for _, v := range a.addrTried[bucket] {
		if oldest == nil || oldest.lastSeen.After(v.lastSeen) {
			oldest = v
		}
	}
	return oldest
}

func (a *addrBook) getNewBucket(netAddr, srcAddr net.IP) int {
	// bitcoind:
	// doublesha256(key + sourcegroup + int64(doublesha256(key + group + sourcegroup))%bucket_per_source_group) % num_new_buckets

	data1 := []byte{}
	data1 = append(data1, a.key[:]...)
	data1 = append(data1, []byte(GroupKey(netAddr))...)
	data1 = append(data1, []byte(GroupKey(srcAddr))...)
	hash1 := chainhash.DoubleHashB(data1)
	hash64 := binary.LittleEndian.Uint64(hash1)
	hash64 %= newBucketsPerGroup
	var hashbuf [8]byte
	binary.LittleEndian.PutUint64(hashbuf[:], hash64)
	data2 := []byte{}
	data2 = append(data2, a.key[:]...)
	data2 = append(data2, GroupKey(srcAddr)...)
	data2 = append(data2, hashbuf[:]...)

	hash2 := chainhash.DoubleHashB(data2)
	return int(binary.LittleEndian.Uint64(hash2) % newBucketCount)
}

func (a *addrBook) getTriedBucket(netAddr net.IP) int {
	// bitcoind hashes this as:
	// doublesha256(key + group + truncate_to_64bits(doublesha256(key)) % buckets_per_group) % num_buckets
	data1 := []byte{}
	data1 = append(data1, a.key[:]...)
	data1 = append(data1, []byte(netAddr)...)
	hash1 := chainhash.DoubleHashB(data1)
	hash64 := binary.LittleEndian.Uint64(hash1)
	hash64 %= triedBucketsPerGroup
	var hashbuf [8]byte
	binary.LittleEndian.PutUint64(hashbuf[:], hash64)
	data2 := []byte{}
	data2 = append(data2, a.key[:]...)
	data2 = append(data2, GroupKey(netAddr)...)
	data2 = append(data2, hashbuf[:]...)

	hash2 := chainhash.DoubleHashB(data2)
	return int(binary.LittleEndian.Uint64(hash2) % triedBucketCount)
}

// AddAddresses adds new addresses to the address manager.  It enforces a max
// number of addresses and silently ignores duplicate addresses.  It is
// safe for concurrent access.
func (a *addrBook) AddAddresses(addrs []*node.NodeInfo, srcAddr *node.NodeInfo) {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	for _, na := range addrs {
		a.updateAddress(na, srcAddr)
	}
}

// AddAddress adds a new address to the address manager.  It enforces a max
// number of addresses and silently ignores duplicate addresses.  It is
// safe for concurrent access.
func (a *addrBook) AddAddress(addr, srcAddr *node.NodeInfo) {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	a.updateAddress(addr, srcAddr)
}

// RemoveAddress
func (a *addrBook) RemoveAddress(key p2pcrypto.PublicKey) {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	id := key.Array()

	ka := a.addrIndex[id]
	if ka == nil {
		return
	}

	for _, b := range a.addrNew {
		if _, ok := b[id]; ok {
			delete(b, id)
			a.nNew--
		}
	}

	for _, b := range a.addrTried {
		if _, ok := b[id]; ok {
			delete(b, id)
			a.nTried--
		}
	}

	delete(a.addrIndex, id)
}

// reset resets the address manager by reinitialising the random source
// and allocating fresh empty bucket storage.
func (a *addrBook) reset() {

	a.addrIndex = make(map[node.ID]*KnownAddress)

	// fill key with bytes from a good random source.
	err := crypto.GetRandomBytesToBuffer(32, a.key[:])
	if err != nil {
		panic(err)
	}

	for i := range a.addrNew {
		a.addrNew[i] = make(map[node.ID]*KnownAddress)
	}
	for i := range a.addrTried {
		a.addrTried[i] = make(map[node.ID]*KnownAddress)
	}
}

// New returns a new bitcoin address manager.
// Use Start to begin processing asynchronous address updates.
func NewAddrBook(localAddress *node.NodeInfo, config config.SwarmConfig, logger log.Log) *addrBook {
	//TODO use config for const params.
	am := addrBook{
		logger:       logger,
		localAddress: localAddress,
		rand:         rand.New(rand.NewSource(time.Now().UnixNano())),
		quit:         make(chan struct{}),
	}
	am.reset()

	if config.PeersFile != "" {

		dataDir, err := filesystem.GetSpacemeshDataDirectoryPath()
		if err == nil {
			am.loadPeers(dataDir + "/" + config.PeersFile)
		} else {
			am.logger.Warning("Skipping loading peers to addrbook, data dir not found err=%v", err)
		}

		am.wg.Add(1)
		go func() { am.saveRoutine(); am.wg.Done() }()
	}
	return &am
}
