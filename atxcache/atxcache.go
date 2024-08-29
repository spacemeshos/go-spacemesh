package atxcache

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/pebble"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"go.uber.org/zap"

	pebbledb "github.com/cockroachdb/pebble"
)

const BATCH_FLUSH_SIZE = 5_000_000

type Cache struct {
	db          *pebble.KvDb
	mu          sync.Mutex
	evicted     types.EpochID
	warmupBatch *pebbledb.Batch
	malicious   map[types.NodeID]struct{}
}

func New(db *pebble.KvDb) *Cache {
	return &Cache{
		db:          db,
		warmupBatch: db.NewBatch(),
		malicious:   make(map[types.NodeID]struct{}),
	}
}

var total uint32

func (c *Cache) FastAdd(
	id types.ATXID,
	node types.NodeID,
	epoch types.EpochID,
	coinbase types.Address,
	weight,
	base,
	height uint64,
	nonce types.VRFPostIndex,
	malicious bool,
) {
	if malicious {
		c.SetMalicious(node)
	}
	c.add(c.warmupBatch, id, node, epoch, coinbase, weight, base, height, nonce)
	if c.warmupBatch.Count() >= BATCH_FLUSH_SIZE {
		total += c.warmupBatch.Count()
		fmt.Println("flushing entries", BATCH_FLUSH_SIZE, "total", total)
		if err := c.warmupBatch.Commit(nil); err != nil {
			panic(err)
		}
		c.warmupBatch.Reset()
	}
}

func (c *Cache) Add(
	id types.ATXID,
	node types.NodeID,
	epoch types.EpochID,
	coinbase types.Address,
	weight,
	base,
	height uint64,
	nonce types.VRFPostIndex,
	malicious bool,
) *ATX {
	if malicious {
		c.SetMalicious(node)
	}
	a := c.add(c.db, id, node, epoch, coinbase, weight, base, height, nonce)
	return a
}

func (c *Cache) AddFromAtx(atx *types.ActivationTx, malicious bool) *ATX {
	if malicious {
		c.SetMalicious(atx.SmesherID)
	}

	return c.add(
		c.db,
		atx.ID(),
		atx.SmesherID,
		atx.TargetEpoch(),
		atx.Coinbase,
		atx.Weight,
		atx.BaseTickHeight,
		atx.TickHeight(),
		atx.VRFNonce,
	)
}

func (c *Cache) iteradd(
	id types.ATXID,
	node types.NodeID,
	epoch types.EpochID,
	coinbase types.Address,
	weight,
	base,
	height uint64,
	nonce types.VRFPostIndex,
) bool {
	c.add(c.warmupBatch, id, node, epoch, coinbase, weight, base, height, nonce)
	if c.warmupBatch.Len() >= BATCH_FLUSH_SIZE {
		// fmt.Println("flushing entries", BATCH_FLUSH_SIZE)
		if err := c.warmupBatch.Commit(nil); err != nil {
			panic(err)
		}
		c.warmupBatch.Reset()
	}

	// never stop
	return false
}

type setter interface {
	Set(key, value []byte, _ *pebbledb.WriteOptions) error
}

func (c *Cache) add(
	setter setter,
	id types.ATXID,
	node types.NodeID,
	epoch types.EpochID,
	coinbase types.Address,
	weight,
	base,
	height uint64,
	nonce types.VRFPostIndex,
) *ATX {
	atx := &ATX{
		Node:       node,
		Coinbase:   coinbase,
		Weight:     weight,
		BaseHeight: base,
		Height:     height,
		Nonce:      nonce,
	}

	key := make([]byte, len(id)+4)
	// prefix must be a big-endian integer so that the db arranges rows lexicographicaly
	binary.BigEndian.PutUint32(key, epoch.Uint32())
	copy(key[4:], id[:])
	value, err := atx.MarshalBinary()
	if err != nil {
		panic(err)
	}

	if c.IsEvicted(epoch) {
		return nil
	}

	if has, err := c.db.Has(key); err != nil {
		panic(err)
	} else if has {
		return nil
	}

	setter.Set(key, value, nil)
	return atx
}

func (c *Cache) getVisit(epoch types.EpochID, atxId types.ATXID, cb func([]byte)) error {
	return c.db.GetVisit(key(epoch, atxId), cb)
}

func (c *Cache) Get(epoch types.EpochID, atxId types.ATXID) *ATX {
	b, err := c.db.Get(key(epoch, atxId))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil
		}
	}
	atx := new(ATX)
	// since unmarshal copies the data from the buffer, we can use getvisit here
	if err := atx.UnmarshalBinary(b); err != nil {
		panic(err)
	}
	return atx
}

func key(e types.EpochID, a types.ATXID) []byte {
	key := make([]byte, 36)
	// prefix must be a big-endian integer so that the db arranges rows lexicographicaly
	binary.BigEndian.PutUint32(key, e.Uint32())
	copy(key[4:], a[:])
	return key
}

func (c *Cache) Flush() {
	c.flush()
}

func (c *Cache) flush() error {
	err := c.warmupBatch.Commit(nil)
	c.warmupBatch.Reset()
	c.warmupBatch = nil

	return err
}

func (c *Cache) AddAtx(target types.EpochID, id types.ATXID, atx *ATX) bool {
	if c.IsEvicted(target) {
		return false
	}

	//if _, exists = ecache.index[id]; exists {
	//return false
	//}

	// ecache.index[id] = atx
	return true
}

func (c *Cache) EvictEpoch(evict types.EpochID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// fmt.Println("evicting epoch", evict)
	if c.isEvicted(evict) {
		// fmt.Println("evicting epoch return early", evict)
		return
	}
	if c.evicted < evict {
		// fmt.Println("evicting epoch set evicted", c.evicted, evict)
		c.evicted = evict
	}
	// fmt.Println("delete range")
	c.db.DeleteRange([]byte{0, 0, 0, 0}, epochPrefix(evict))

	//d.evicted.Store(evict.Uint32())
	//}
	//for epoch := range d.epochs {
	//if epoch <= evict {
	//delete(d.epochs, epoch)
	//atxsCounter.DeleteLabelValues(epoch.String())
	//}
	//}
}

func (c *Cache) isEvicted(e types.EpochID) bool {
	return c.evicted >= e
}

func (c *Cache) IsEvicted(e types.EpochID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.isEvicted(e)
}

func (c *Cache) Evicted() types.EpochID {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.evicted
}

func epochPrefix(e types.EpochID) []byte {
	key := make([]byte, 4)
	// prefix must be a big-endian integer so that the db arranges rows lexicographicaly
	binary.BigEndian.PutUint32(key, e.Uint32())
	return key
}

func (c *Cache) IterateInEpoch(epoch types.EpochID, fn func(types.ATXID, *ATX), onlyNonMalicious bool) {
	var nodeID types.NodeID // avoid multiple allocations
	c.db.IterPrefix(epochPrefix(epoch), func(k, v []byte) bool {
		// the values in the callback signature are not copies, so if we need to
		// have them deserialized it must copy them
		if onlyNonMalicious {
			copy(nodeID[:], v[:32])
			if c.IsMalicious(nodeID) {
				return false
			}
		}

		atx := new(ATX)
		atx.UnmarshalBinary(v)

		var atxId types.ATXID
		copy(atxId[:], k[4:])
		fn(atxId, atx)
		return false
	})
}

func (c *Cache) IterateHighTicksInEpoch(target types.EpochID, fn func(types.ATXID) bool) {
	type candidate struct {
		id  types.ATXID
		ATX *ATX
	}

	candidates := make([]candidate, 0, 6_000_000)
	c.IterateInEpoch(target, func(id types.ATXID, atx *ATX) {
		candidates = append(candidates, candidate{id: id, ATX: atx})
	}, true)

	slices.SortFunc(candidates, func(a, b candidate) int {
		switch {
		case a.ATX.Height < b.ATX.Height:
			return 1
		case a.ATX.Height > b.ATX.Height:
			return -1
		}
		return 0
	})

	for _, c := range candidates {
		if cont := fn(c.id); !cont {
			return
		}
	}
}

func (c *Cache) MissingInEpoch(epoch types.EpochID, atxs []types.ATXID) []types.ATXID {
	var missing []types.ATXID
	for _, id := range atxs {
		if has, err := c.db.Has(key(epoch, id)); err != nil {
			panic(err)
		} else if !has {
			// this is idiotic. we should just reslice the original
			// atx slice to remove the element and decrement the index
			// of a for loop
			missing = append(missing, id)
		}
	}
	return missing
}

func (c *Cache) IsMalicious(node types.NodeID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, exists := c.malicious[node]
	return exists
}

func (c *Cache) MaliciousIdentities() []types.NodeID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return maps.Keys(c.malicious)
}

func (c *Cache) SetMalicious(node types.NodeID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.malicious[node] = struct{}{}
}

func compareAtxId(a, b types.ATXID) int {
	return bytes.Compare(a[:], b[:])
}

// WeightForSet computes total weight of atxs in the set and returned array with
// atxs in the set that weren't used.
func (c *Cache) WeightForSet(epoch types.EpochID, set []types.ATXID) (uint64, []bool) {
	used := make([]bool, len(set))
	// start := time.Now()
	slices.SortFunc(set, compareAtxId)
	// fmt.Println("slice sorted, took", time.Now().Sub(start))
	var weight uint64
	i := 0
	c.db.IterPrefix(epochPrefix(epoch), func(k, v []byte) bool {
	CMP:
		switch bytes.Compare(k[4:], set[i][:]) {
		case 0:
			w := binary.LittleEndian.Uint64(v[32+24:])
			weight += w
			used[i] = true
			if i == len(set)-1 {
				// done, tell the iterator to stop
				return true
			}
			i++
			return false
		case -1:
			// left smaller than right
			// we need to advance the iterator, since it is "behind"
			// the key
			return false
		case 1:
			// right is smaller than iterator, we therefore want to advance i
			// and then check again
			i++
			goto CMP
		}
		return false
	})

	return weight, used
}

type ATX struct {
	Node       types.NodeID
	Coinbase   types.Address
	Weight     uint64
	BaseHeight uint64
	Height     uint64
	Nonce      types.VRFPostIndex
}

func (a *ATX) MarshalBinary() ([]byte, error) {
	out := make([]byte, 32+24+8+8+8+8)
	copy(out, a.Node[:])
	copy(out[32:], a.Coinbase[:])
	binary.LittleEndian.PutUint64(out[32+24:], a.Weight)
	binary.LittleEndian.PutUint64(out[32+24+8:], a.BaseHeight)
	binary.LittleEndian.PutUint64(out[32+24+8+8:], a.Height)
	binary.LittleEndian.PutUint64(out[32+24+8+8+8:], uint64(a.Nonce))
	return out, nil
}

func (a *ATX) UnmarshalBinary(data []byte) error {
	copy(a.Node[:], data)
	copy(a.Coinbase[:], data[32:])
	a.Weight = binary.LittleEndian.Uint64(data[32+24:])
	a.BaseHeight = binary.LittleEndian.Uint64(data[32+24+8:])
	a.Height = binary.LittleEndian.Uint64(data[32+24+8+8:])
	a.Nonce = types.VRFPostIndex(binary.LittleEndian.Uint64(data[32+24+8+8+8:]))

	return nil
}

func Warm(db sql.StateDatabase, keep types.EpochID, logger *zap.Logger) (*Cache, error) {
	cache := New(nil)
	tx, err := db.Tx(context.Background())
	if err != nil {
		return nil, err
	}
	defer tx.Release()
	if err := Warmup(tx, cache, keep, logger); err != nil {
		return nil, fmt.Errorf("warmup %w", err)
	}
	return cache, nil
}

func Warmup(db sql.Executor, cache *Cache, keep types.EpochID, logger *zap.Logger) error {
	latest, err := atxs.LatestEpoch(db)
	if err != nil {
		return err
	}
	applied, err := layers.GetLastApplied(db)
	if err != nil {
		return err
	}
	var evict types.EpochID
	if applied.GetEpoch() > keep {
		evict = applied.GetEpoch() - keep - 1
	}
	cache.EvictEpoch(evict)

	from := cache.Evicted()
	logger.Info("Reading ATXs from DB",
		zap.Uint32("from epoch", from.Uint32()),
		zap.Uint32("to epoch", latest.Uint32()),
	)
	start := time.Now()
	err = atxs.IterateAtxsData(db, cache.Evicted(), latest, cache.iteradd)
	if err != nil {
		return fmt.Errorf("warming up atxdata with ATXs: %w", err)
	}
	cache.flush()
	logger.Info("Finished reading ATXs. Starting reading malfeasance", zap.Duration("duration", time.Since(start)))
	start = time.Now()
	err = identities.IterateMalicious(db, func(_ int, id types.NodeID) error {
		cache.SetMalicious(id)
		return nil
	})
	if err != nil {
		return fmt.Errorf("warming up atxdata with malfeasance: %w", err)
	}
	logger.Info("Finished reading malfeasance", zap.Duration("duration", time.Since(start)))
	return nil
}
