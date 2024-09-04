package multipeer

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

type syncResult struct {
	s   Syncer
	err error
}

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
	syncers      []Syncer
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

func (s *splitSync) startPeerSync(ctx context.Context, p p2p.Peer, sr *syncRange) {
	syncer := s.syncBase.Derive(p)
	sr.numSyncers++
	s.numRunning++
	doneCh := make(chan struct{})
	s.eg.Go(func() error {
		defer close(doneCh)
		err := syncer.Sync(ctx, sr.x, sr.y)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case s.resCh <- syncResult{s: syncer, err: err}:
			return nil
		}
	})
	gpTimer := s.clock.After(s.gracePeriod)
	s.eg.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-doneCh:
		case <-gpTimer:
			// if another peer finishes it part early, let
			// it pick up this range
			s.slowRangeCh <- sr
		}
		return nil
	})
}

func (s *splitSync) handleSyncResult(r syncResult) error {
	sr, found := s.syncMap[r.s.Peer()]
	if !found {
		panic("BUG: error in split sync syncMap handling")
	}
	s.numRunning--
	delete(s.syncMap, r.s.Peer())
	sr.numSyncers--
	if r.err != nil {
		s.numPeers--
		s.failedPeers[r.s.Peer()] = struct{}{}
		s.logger.Debug("remove failed peer",
			zap.Stringer("peer", r.s.Peer()),
			zap.Int("numPeers", s.numPeers),
			zap.Int("numRemaining", s.numRemaining),
			zap.Int("numRunning", s.numRunning),
			zap.Int("availPeers", len(s.syncPeers)))
		if s.numPeers == 0 && s.numRemaining != 0 {
			return errors.New("all peers dropped before full sync has completed")
		}
		if sr.numSyncers == 0 {
			// QQQQQ: it has been popped!!!!
			// prioritize the syncRange for resync after failed
			// sync with no active syncs remaining
			s.sq.update(sr, time.Time{})
		}
	} else {
		sr.done = true
		s.syncPeers = append(s.syncPeers, r.s.Peer())
		s.numRemaining--
		s.logger.Debug("peer synced successfully",
			zap.Stringer("peer", r.s.Peer()),
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

func (s *splitSync) sync(ctx context.Context) error {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var syncCtx context.Context
	s.eg, syncCtx = errgroup.WithContext(sctx)
	for s.numRemaining > 0 {
		var sr *syncRange
		for {
			s.logger.Debug("QQQQQ: wait sr")
			sr := s.sq.popRange()
			if sr != nil {
				if sr.done {
					continue
				}
				p := s.nextPeer()
				s.syncMap[p] = sr
				s.startPeerSync(syncCtx, p, sr)
			}
			break
		}
		s.clearDeadPeers()
		for s.numRemaining > 0 && (s.sq.empty() || len(s.syncPeers) == 0) {
			s.logger.Debug("QQQQQ: loop")
			if s.numRunning == 0 && len(s.syncPeers) == 0 {
				return errors.New("all peers dropped before full sync has completed")
			}
			select {
			case sr = <-s.slowRangeCh:
				// push this syncRange to the back of the queue
				s.sq.update(sr, s.clock.Now())
			case <-syncCtx.Done():
				return syncCtx.Err()
			case r := <-s.resCh:
				if err := s.handleSyncResult(r); err != nil {
					return err
				}
			}
		}
		s.logger.Debug("QQQQQ: after loop")
	}
	s.logger.Debug("QQQQQ: wg wait")
	return s.eg.Wait()
}
