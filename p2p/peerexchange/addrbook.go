package peerexchange

import (
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
	"net"
	"path/filepath"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"

	"github.com/spacemeshos/go-spacemesh/log"
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
	// to a getAddr (in practice the most addresses we will return from a
	// call to AddressCache()).
	getAddrMax = 300

	// getAddrPercent is the percentage of total addresses known that we
	// will share with a call to AddressCache.
	getAddrPercent = 23
)

// newAddrBook returns a new address manager.
// Use Start to begin processing asynchronous address updates.
func newAddrBook(cfg Config, logger log.Log) *addrBook {
	// TODO use config for const params.
	path := ""
	if len(cfg.DataDir) != 0 {
		path = filepath.Join(cfg.DataDir, peersFileName)
	}
	am := addrBook{
		logger: logger,
		path:   path,
		rand:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	am.reset()
	am.loadPeers(am.path)
	return &am
}

// addrBook provides a concurrency safe address manager for caching potential
// peers on the network. based on bitcoin's addrBook.
type addrBook struct {
	logger log.Log
	path   string

	mu        sync.RWMutex
	rand      *rand.Rand
	key       [32]byte
	addrIndex map[peer.ID]*knownAddress
	addrNew   [newBucketCount]map[peer.ID]*knownAddress
	addrTried [triedBucketCount]map[peer.ID]*knownAddress

	nTried int
	nNew   int
}

// updateAddress is a helper function to either update an address already known
// to the address manager, or to add the address if not already known.
func (a *addrBook) updateAddress(addr, src *addrInfo) {
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
		// TODO: only update addresses periodically.
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
		if ka.refs == newBucketsPerAddress {
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
	if len(a.addrNew[bucket]) > newBucketSize {
		a.logger.Debug("new bucket is full, expiring old")
		a.expireNew(bucket)
	}

	// Add to new bucket.
	ka.refs++
	a.addrNew[bucket][addr.ID] = ka

	a.logger.Debug("added new address %s for a total of %d addresses", addr.RawAddr, a.nTried+a.nNew)
}

// Lookup searches for an address using a public key. returns *Info.
func (a *addrBook) Lookup(addr peer.ID) *addrInfo {
	a.mu.Lock()
	d := a.lookup(addr)
	a.mu.Unlock()
	if d == nil {
		return nil
	}
	return d.Addr
}

func (a *addrBook) lookup(addr peer.ID) *knownAddress {
	return a.addrIndex[addr]
}

// moves a knownaddress to a tried bucket.
func (a *addrBook) toTried(ka *knownAddress) {
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
	if len(a.addrTried[bucket]) < triedBucketSize {
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
	if len(a.addrNew[newBucket]) >= newBucketSize {
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
// the older of them.
func (a *addrBook) pickTried(bucket int) *knownAddress {
	var oldest *knownAddress
	for _, ka := range a.addrTried[bucket] {
		if oldest == nil || oldest.LastSeen.After(ka.LastSeen) {
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
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.numAddresses()
}

// AddressCache returns the current address cache.  It must be treated as
// read-only (but since it is a copy now, this is not as dangerous).
func (a *addrBook) AddressCache() []*addrInfo {
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
	return allAddr[:numAddresses]
}

// getAddresses returns all of the addresses currently found within the
// manager's address cache.
func (a *addrBook) getAddresses() []*addrInfo {
	a.mu.Lock()
	defer a.mu.Unlock()

	addrIndexLen := len(a.addrIndex)
	if addrIndexLen == 0 {
		return nil
	}

	addrs := make([]*addrInfo, 0, addrIndexLen)
	for _, v := range a.addrIndex {
		addrs = append(addrs, v.Addr)
	}

	return addrs
}

func (a *addrBook) GetAllAddressesUsedBefore(date time.Time) []*addrInfo {
	a.mu.Lock()
	defer a.mu.Unlock()

	addrIndexLen := len(a.addrIndex)
	if addrIndexLen == 0 {
		return nil
	}

	addrs := make([]*addrInfo, 0, addrIndexLen)
	for _, v := range a.addrIndex {
		if v.LastSeen.Before(date) {
			addrs = append(addrs, v.Addr)
		}
	}
	return addrs
}

// expireNew makes space in the new buckets by expiring the really bad entries.
// If no bad entries are available we look at a few and remove the oldest.
func (a *addrBook) expireNew(bucket int) {
	// First see if there are any entries that are so bad we can just throw
	// them away. otherwise we throw away the oldest entry in the cache.
	// Bitcoind here chooses four random and just throws the oldest of
	// those away, but we keep track of oldest in the initial traversal and
	// use that information instead.
	var oldest *knownAddress
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
			delete(a.addrIndex, key)
		}
	}
}

func doubleHash(buf []byte) []byte {
	first := sha256.Sum256(buf)
	second := sha256.Sum256(first[:])
	return second[:]
}

func (a *addrBook) getNewBucket(netAddr, srcAddr net.IP) int {
	// bitcoind:
	// doublesha256(key + sourcegroup + int64(doublesha256(key + group + sourcegroup))%bucket_per_source_group) % num_new_buckets

	var data1 []byte
	data1 = append(data1, a.key[:]...)
	data1 = append(data1, []byte(GroupKey(netAddr))...)
	data1 = append(data1, []byte(GroupKey(srcAddr))...)
	hash1 := doubleHash(data1)
	hash64 := binary.LittleEndian.Uint64(hash1)
	hash64 %= newBucketsPerGroup
	var hashbuf [8]byte
	binary.LittleEndian.PutUint64(hashbuf[:], hash64)
	var data2 []byte
	data2 = append(data2, a.key[:]...)
	data2 = append(data2, GroupKey(srcAddr)...)
	data2 = append(data2, hashbuf[:]...)

	hash2 := doubleHash(data2)
	return int(binary.LittleEndian.Uint64(hash2) % newBucketCount)
}

func (a *addrBook) getTriedBucket(netAddr net.IP) int {
	// bitcoind hashes this as:
	// doublesha256(key + group + truncate_to_64bits(doublesha256(key)) % buckets_per_group) % num_buckets
	var data1 []byte
	data1 = append(data1, a.key[:]...)
	data1 = append(data1, []byte(netAddr)...)
	hash1 := doubleHash(data1)
	hash64 := binary.LittleEndian.Uint64(hash1)
	hash64 %= triedBucketsPerGroup
	var hashbuf [8]byte
	binary.LittleEndian.PutUint64(hashbuf[:], hash64)
	var data2 []byte
	data2 = append(data2, a.key[:]...)
	data2 = append(data2, GroupKey(netAddr)...)
	data2 = append(data2, hashbuf[:]...)

	hash2 := doubleHash(data2)
	return int(binary.LittleEndian.Uint64(hash2) % triedBucketCount)
}

// AddAddresses adds new addresses to the address manager.  It enforces a max
// number of addresses and silently ignores duplicate addresses.  It is
// safe for concurrent access.
func (a *addrBook) AddAddresses(addrs []*addrInfo, src *addrInfo) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, na := range addrs {
		a.updateAddress(na, src)
	}
}

// AddAddress adds a new address to the address manager.  It enforces a max
// number of addresses and silently ignores duplicate addresses.  It is
// safe for concurrent access.
func (a *addrBook) AddAddress(addr, src *addrInfo) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.updateAddress(addr, src)
}

// Attempt increases the given address' attempt counter and updates
// the last attempt time.
func (a *addrBook) Attempt(pid peer.ID) {
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
func (a *addrBook) Good(pid peer.ID) {
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

// RemoveAddress.
func (a *addrBook) RemoveAddress(pid peer.ID) {
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

	delete(a.addrIndex, pid)
}

// reset resets the address manager by reinitialising the random source
// and allocating fresh empty bucket storage.
func (a *addrBook) reset() {
	a.addrIndex = make(map[peer.ID]*knownAddress)

	// fill key with bytes from a good random source.
	_, err := crand.Read(a.key[:])
	if err != nil {
		a.logger.Panic("Error generating random bytes %v", err)
	}

	for i := range a.addrNew {
		a.addrNew[i] = make(map[peer.ID]*knownAddress)
	}
	for i := range a.addrTried {
		a.addrTried[i] = make(map[peer.ID]*knownAddress)
	}
}
