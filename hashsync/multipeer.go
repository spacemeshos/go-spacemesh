package hashsync

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
)

// type DataHandler func(context.Context, types.Hash32, p2p.Peer, any) error

// type dataItem struct {
// 	key   types.Hash32
// 	value any
// }

// type dataItemHandler func(di dataItem)

// type derivedStore struct {
// 	ItemStore
// 	handler dataItemHandler
// 	// itemCh chan dataItem
// 	// // TODO: don't embed context in the struct
// 	// ctx context.Context
// }

// func (s *derivedStore) Add(k Ordered, v any) {
// 	s.ItemStore.Add(k, v)
// 	s.handler(dataItem{key: k.(types.Hash32), value: v})
// 	// select {
// 	// case <-s.ctx.Done():
// 	// case s.itemCh <- dataItem{key: k.(types.Hash32), value: v}:
// 	// }
// }

type probeResult struct {
	probed   map[p2p.Peer]int
	minCount int
	maxCount int
}

// type peerReconciler struct {
// 	st SyncTree
// }

type MultiPeerReconcilerOpt func(mpr *MultiPeerReconciler)

func WithMinFullSyncCount(count int) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.minPartSyncCount = count
	}
}

func WithMinFullFraction(frac float64) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.minFullFraction = frac
	}
}

// func WithMinPartSyncPeers(n int) MultiPeerReconcilerOpt {
// 	return func(mpr *MultiPeerReconciler) {
// 		mpr.minPartSyncPeers = n
// 	}
// }

// func WithPeerSyncTimeout(t time.Duration) MultiPeerReconcilerOpt {
// 	return func(mpr *MultiPeerReconciler) {
// 		mpr.peerSyncTimeout = t
// 	}
// }

func WithSplitSyncGracePeriod(t time.Duration) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.splitSyncGracePeriod = t
	}
}

func withClock(clock clockwork.Clock) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.clock = clock
	}
}

type MultiPeerReconciler struct {
	logger zap.Logger
	// minPartSyncPeers    int
	minPartSyncCount     int
	minFullFraction      float64
	splitSyncGracePeriod time.Duration
	// peerSyncTimeout  time.Duration
	syncBase syncBase
	peerLock sync.Mutex
	peers    map[p2p.Peer]struct{}
	clock    clockwork.Clock
}

func NewMultiPeerReconciler(logger zap.Logger, syncBase syncBase, opts ...MultiPeerReconcilerOpt) *MultiPeerReconciler {
	return &MultiPeerReconciler{
		// minPartSyncPeers:    2,
		minPartSyncCount:     1000,
		minFullFraction:      0.95,
		splitSyncGracePeriod: time.Minute,
		syncBase:             syncBase,
		clock:                clockwork.NewRealClock(),
	}
}

func (mpr *MultiPeerReconciler) addPeer(p p2p.Peer) {
	mpr.peerLock.Lock()
	defer mpr.peerLock.Unlock()
	mpr.peers[p] = struct{}{}
}

func (mpr *MultiPeerReconciler) removePeer(p p2p.Peer) {
	mpr.peerLock.Lock()
	defer mpr.peerLock.Unlock()
	delete(mpr.peers, p)
}

func (mpr *MultiPeerReconciler) numPeers() int {
	mpr.peerLock.Lock()
	defer mpr.peerLock.Unlock()
	return len(mpr.peers)
}

func (mpr *MultiPeerReconciler) listPeers() []p2p.Peer {
	mpr.peerLock.Lock()
	defer mpr.peerLock.Unlock()
	return maps.Keys(mpr.peers)
}

func (mpr *MultiPeerReconciler) havePeer(p p2p.Peer) bool {
	mpr.peerLock.Lock()
	defer mpr.peerLock.Unlock()
	_, found := mpr.peers[p]
	return found
}

func (mpr *MultiPeerReconciler) probePeers(ctx context.Context) (*probeResult, error) {
	var pr probeResult
	for _, p := range mpr.listPeers() {
		count, err := mpr.syncBase.probe(ctx, p)
		if err != nil {
			log.Warning("error probing the peer", zap.Any("peer", p), zap.Error(err))
			if errors.Is(err, context.Canceled) {
				return nil, err
			}
			continue
		}
		if pr.probed == nil {
			pr.probed = map[p2p.Peer]int{
				p: count,
			}
			pr.minCount = count
			pr.maxCount = count
		} else {
			pr.probed[p] = count
			if count < pr.minCount {
				pr.minCount = count
			}
			if count > pr.maxCount {
				pr.maxCount = count
			}
		}
	}
	return &pr, nil
}

// func (mpr *MultiPeerReconciler) splitSync(ctx context.Context, peers []p2p.Peer) error {
// 	// Use priority queue. Higher priority = more time since started syncing
// 	// Highest priority = not started syncing yet
// 	// Mark syncRange as synced when it's done, next time it's popped from the queue,
// 	// it will be dropped
// 	// When picking up an entry which is already being synced, start with
// 	// SyncTree of the entry
// 	// TODO: when all of the ranges are synced at least once, just return.
// 	// The remaining syncs will be canceled
// 	// TODO: when no available peers remain, return failure
// 	if len(peers) == 0 {
// 		panic("BUG: no peers passed to splitSync")
// 	}
// 	syncCtx, cancel := context.WithCancel(ctx)
// 	defer cancel()
// 	delim := getDelimiters(len(peers))
// 	sq := make(syncQueue, len(peers))
// 	var y types.Hash32
// 	for n := range sq {
// 		x := y
// 		if n == len(peers)-1 {
// 			y = types.Hash32{}
// 		} else {
// 			y = delim[n]
// 		}
// 		sq[n] = &syncRange{
// 			x: x,
// 			y: y,
// 		}
// 	}
// 	heap.Init(&sq)
// 	peers = slices.Clone(peers)
// 	resCh := make(chan syncResult)
// 	syncMap := make(map[p2p.Peer]*syncRange)
// 	numRunning := 0
// 	numRemaining := len(peers)
// 	numPeers := len(peers)
// 	needGracePeriod := true
// 	for numRemaining > 0 {
// 		p := peers[0]
// 		peers = peers[1:]
// 		var sr *syncRange
// 		for len(sq) != 0 {
// 			sr = heap.Pop(&sq).(*syncRange)
// 			if !sr.done {
// 				break
// 			}
// 			sr = nil
// 		}
// 		if sr == nil {
// 			panic("BUG: bad syncRange accounting in splitSync")
// 		}
// 		syncMap[p] = sr
// 		var s syncer
// 		if len(sr.syncers) != 0 {
// 			// derive from an existing syncer to get sync against
// 			// more up-to-date data
// 			s = sr.syncers[len(sr.syncers)-1].derive(p)
// 		} else {
// 			s = mpr.syncBase.derive(p)
// 		}
// 		sr.syncers = append(sr.syncers, s)
// 		numRunning++
// 		// push this syncRange to the back of the queue as a fresh sync
// 		// is just starting
// 		sq.update(sr, mpr.clock.Now())
// 		go func() {
// 			err := s.sync(syncCtx, &sr.x, &sr.y)
// 			select {
// 			case <-syncCtx.Done():
// 			case resCh <- syncResult{s: s, err: err}:
// 			}
// 		}()

// 		peers := slices.DeleteFunc(peers, func(p p2p.Peer) bool {
// 			return !mpr.havePeer(p)
// 		})

// 		// Grace period: after at least one syncer finishes, wait a bit
// 		// before assigning it another range to avoid unneeded traffic.
// 		// The grace period ends if any of the syncers fail
// 		var gpTimer <-chan time.Time
// 		if needGracePeriod {
// 			gpTimer = mpr.clock.After(mpr.splitSyncGracePeriod)
// 		}
// 		for needGracePeriod && len(peers) == 0 {
// 			if numRunning == 0 {
// 				return errors.New("all peers dropped before full sync has completed")
// 			}

// 			var r syncResult
// 			select {
// 			case <-syncCtx.Done():
// 				return syncCtx.Err()
// 			case r = <-resCh:
// 			case <-gpTimer:
// 				needGracePeriod = false
// 			}

// 			sr, found := syncMap[s.peer()]
// 			if !found {
// 				panic("BUG: error in split sync syncMap handling")
// 			}
// 			numRunning--
// 			delete(syncMap, s.peer())
// 			n := slices.Index(sr.syncers, s)
// 			if n < 0 {
// 				panic("BUG: bad syncers in syncRange")
// 			}
// 			sr.syncers = slices.Delete(sr.syncers, n, n+1)
// 			if r.err != nil {
// 				numPeers--
// 				mpr.RemovePeer(s.peer())
// 				if numPeers == 0 && numRemaining != 0 {
// 					return errors.New("all peers dropped before full sync has completed")
// 				}
// 				if len(sr.syncers) == 0 {
// 					// prioritize the syncRange for resync after failed
// 					// sync with no active syncs remaining
// 					sq.update(sr, time.Time{})
// 				}
// 				needGracePeriod = false
// 			} else {
// 				sr.done = true
// 				peers = append(peers, s.peer())
// 				numRemaining--
// 			}
// 		}
// 	}

// 	return nil
// }

func (mpr *MultiPeerReconciler) run(ctx context.Context) error {
	// States:
	// A. No peers -> do nothing.
	//    Got any peers => B
	// B. Low on peers. Wait for more to appear
	//    Lost all peers => A
	//    Got enough peers => C
	//    Timeout => C
	// C. Probe the peers. Use successfully probed ones in states D/E
	//    All probes failed => A
	//    All are low on count (minPartSyncCount) => E
	//    Some have substantially higher count (minFullFraction) => D
	//    Otherwise => E
	// D. Bounded sync. Subdivide the range by peers and start syncs.
	//      Use peers with > minPartSyncCount
	//      Wait for all the syncs to complete/fail
	//    All syncs succeeded => A
	//    Any syncs failed => A
	// E. Full sync. Run full syncs against each peer
	//    All syncs completed (success / fail) => F
	// F. Wait. Pause for sync interval
	//    Timeout => A
	panic("TBD")
}
