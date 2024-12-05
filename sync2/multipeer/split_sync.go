package multipeer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

type syncResult struct {
	ps  PeerSyncer
	err error
}

// splitSync is a synchronization implementation that synchronizes the set against
// multiple peers in parallel, but splits the synchronization into ranges and assigns
// each range to a single peer.
// The splitting is done in such way that only maxDepth high bits of the key are non-zero,
// which helps with radix tree based OrderedSet implementation.
type splitSync struct {
	logger       *zap.Logger
	syncBase     SyncBase
	peers        *peers.Peers
	syncPeers    []p2p.Peer
	gracePeriod  time.Duration
	clock        clockwork.Clock
	sq           syncQueue
	resCh        chan syncResult
	slowRangeCh  chan *syncRange
	syncMap      map[p2p.Peer]*syncRange
	failedPeers  map[p2p.Peer]struct{}
	numRunning   int
	numRemaining int
	numPeers     int
	eg           *errgroup.Group
}

func newSplitSync(
	logger *zap.Logger,
	syncBase SyncBase,
	peers *peers.Peers,
	syncPeers []p2p.Peer,
	gracePeriod time.Duration,
	clock clockwork.Clock,
	keyLen, maxDepth int,
) *splitSync {
	if len(syncPeers) == 0 {
		panic("BUG: no peers passed to splitSync")
	}
	return &splitSync{
		logger:       logger,
		syncBase:     syncBase,
		peers:        peers,
		syncPeers:    syncPeers,
		gracePeriod:  gracePeriod,
		clock:        clock,
		sq:           newSyncQueue(len(syncPeers), keyLen, maxDepth),
		resCh:        make(chan syncResult),
		syncMap:      make(map[p2p.Peer]*syncRange),
		failedPeers:  make(map[p2p.Peer]struct{}),
		numRemaining: len(syncPeers),
		numPeers:     len(syncPeers),
		slowRangeCh:  make(chan *syncRange),
	}
}

func (s *splitSync) nextPeer() p2p.Peer {
	if len(s.syncPeers) == 0 {
		panic("BUG: no peers")
	}
	p := s.syncPeers[0]
	s.syncPeers = s.syncPeers[1:]
	return p
}

func (s *splitSync) startPeerSync(ctx context.Context, p p2p.Peer, sr *syncRange) error {
	sr.NumSyncers++
	s.numRunning++
	doneCh := make(chan struct{})
	s.eg.Go(func() error {
		if err := s.syncBase.WithPeerSyncer(ctx, p, func(ps PeerSyncer) error {
			err := ps.Sync(ctx, sr.X, sr.Y)
			close(doneCh)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case s.resCh <- syncResult{ps: ps, err: err}:
				return nil
			}
		}); err != nil {
			return fmt.Errorf("sync peer %s: %w", p, err)
		}
		return nil
	})
	gpTimer := s.clock.After(s.gracePeriod)
	s.eg.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-doneCh:
		case <-gpTimer:
			// if another peer finishes its part early, let
			// it pick up this range
			select {
			case <-ctx.Done():
				return ctx.Err()
			case s.slowRangeCh <- sr:
			}
		}
		return nil
	})
	return nil
}

func (s *splitSync) handleSyncResult(r syncResult) error {
	sr, found := s.syncMap[r.ps.Peer()]
	if !found {
		panic("BUG: error in split sync syncMap handling")
	}
	s.numRunning--
	delete(s.syncMap, r.ps.Peer())
	sr.NumSyncers--
	if r.err != nil {
		s.numPeers--
		s.failedPeers[r.ps.Peer()] = struct{}{}
		s.logger.Debug("remove failed peer",
			zap.Stringer("peer", r.ps.Peer()),
			zap.Int("numPeers", s.numPeers),
			zap.Int("numRemaining", s.numRemaining),
			zap.Int("numRunning", s.numRunning),
			zap.Int("availPeers", len(s.syncPeers)))
		if s.numPeers == 0 && s.numRemaining != 0 {
			return errors.New("all peers dropped before full sync has completed")
		}
		if sr.NumSyncers == 0 {
			// prioritize the syncRange for resync after failed
			// sync with no active syncs remaining
			s.sq.Update(sr, time.Time{})
		}
	} else {
		sr.Done = true
		s.syncPeers = append(s.syncPeers, r.ps.Peer())
		s.numRemaining--
		s.logger.Debug("peer synced successfully",
			log.ZShortStringer("x", sr.X),
			log.ZShortStringer("y", sr.Y),
			zap.Stringer("peer", r.ps.Peer()),
			zap.Int("numPeers", s.numPeers),
			zap.Int("numRemaining", s.numRemaining),
			zap.Int("numRunning", s.numRunning),
			zap.Int("availPeers", len(s.syncPeers)))
	}

	return nil
}

func (s *splitSync) clearDeadPeers() {
	s.syncPeers = slices.DeleteFunc(s.syncPeers, func(p p2p.Peer) bool {
		if !s.peers.Contains(p) {
			return true
		}
		_, failed := s.failedPeers[p]
		return failed
	})
}

func (s *splitSync) Sync(ctx context.Context) error {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var syncCtx context.Context
	s.eg, syncCtx = errgroup.WithContext(sctx)
	for s.numRemaining > 0 {
		var sr *syncRange
		for {
			sr := s.sq.PopRange()
			if sr == nil {
				break
			}
			if sr.Done {
				continue
			}
			p := s.nextPeer()
			s.syncMap[p] = sr
			if err := s.startPeerSync(syncCtx, p, sr); err != nil {
				return err
			}
			break
		}
		s.clearDeadPeers()
		for s.numRemaining > 0 && (s.sq.empty() || len(s.syncPeers) == 0) {
			if s.numRunning == 0 && len(s.syncPeers) == 0 {
				return errors.New("all peers dropped before split sync has completed")
			}
			select {
			case sr = <-s.slowRangeCh:
				// Push this syncRange to the back of the queue.
				// There's some chance that the peer managed to complete
				// the sync while the range was still sitting in the
				// channel, so we double-check if it's done.
				if !sr.Done {
					s.logger.Debug("slow peer, reassigning the range",
						log.ZShortStringer("x", sr.X), log.ZShortStringer("y", sr.Y))
					s.sq.Update(sr, s.clock.Now())
				} else {
					s.logger.Debug("slow peer, NOT reassigning the range: DONE",
						log.ZShortStringer("x", sr.X), log.ZShortStringer("y", sr.Y))
				}
			case <-syncCtx.Done():
				return syncCtx.Err()
			case r := <-s.resCh:
				if err := s.handleSyncResult(r); err != nil {
					return err
				}
			}
		}
	}
	// Stop late peers that didn't manage to sync their ranges in time.
	// The ranges were already reassigned to other peers and successfully
	// synced by this point.
	cancel()
	err := s.eg.Wait()
	if s.numRemaining == 0 {
		// If all the ranges are synced, the split sync is considered successful
		// even if some peers failed to sync their ranges, so that these ranges
		// got synced by other peers.
		return nil
	}
	return err
}
