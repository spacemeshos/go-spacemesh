package hashsync

import (
	"context"
	"encoding/binary"
	"errors"
	"slices"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

type syncResult struct {
	s   syncer
	err error
}

type splitSync struct {
	logger       *zap.Logger
	syncBase     syncBase
	peerSet      peerSet
	peers        []p2p.Peer
	gracePeriod  time.Duration
	clock        clockwork.Clock
	sq           syncQueue
	resCh        chan syncResult
	slowRangeCh  chan *syncRange
	syncMap      map[p2p.Peer]*syncRange
	numRunning   int
	numRemaining int
	numPeers     int
	syncers      []syncer
	eg           *errgroup.Group
}

func newSplitSync(
	logger *zap.Logger,
	syncBase syncBase,
	peerSet peerSet,
	peers []p2p.Peer,
	gracePeriod time.Duration,
	clock clockwork.Clock,
) *splitSync {
	if len(peers) == 0 {
		panic("BUG: no peers passed to splitSync")
	}
	return &splitSync{
		logger:       logger,
		syncBase:     syncBase,
		peerSet:      peerSet,
		peers:        peers,
		gracePeriod:  gracePeriod,
		clock:        clock,
		sq:           newSyncQueue(len(peers)),
		resCh:        make(chan syncResult),
		syncMap:      make(map[p2p.Peer]*syncRange),
		numRemaining: len(peers),
		numPeers:     len(peers),
	}
}

func (s *splitSync) nextPeer() p2p.Peer {
	if len(s.peers) == 0 {
		panic("BUG: no peers")
	}
	p := s.peers[0]
	s.peers = s.peers[1:]
	return p
}

func (s *splitSync) startPeerSync(ctx context.Context, p p2p.Peer, sr *syncRange) {
	syncer := s.syncBase.derive(p)
	sr.numSyncers++
	s.numRunning++
	doneCh := make(chan struct{})
	s.eg.Go(func() error {
		defer close(doneCh)
		err := syncer.sync(ctx, &sr.x, &sr.y)
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
	sr, found := s.syncMap[r.s.peer()]
	if !found {
		panic("BUG: error in split sync syncMap handling")
	}
	s.numRunning--
	delete(s.syncMap, r.s.peer())
	sr.numSyncers--
	if r.err != nil {
		s.numPeers--
		s.peerSet.removePeer(r.s.peer())
		s.logger.Debug("remove failed peer",
			zap.Stringer("peer", r.s.peer()),
			zap.Int("numPeers", s.numPeers),
			zap.Int("numRemaining", s.numRemaining),
			zap.Int("numRunning", s.numRunning),
			zap.Int("availPeers", len(s.peers)))
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
		s.peers = append(s.peers, r.s.peer())
		s.numRemaining--
		s.logger.Debug("peer synced successfully",
			zap.Stringer("peer", r.s.peer()),
			zap.Int("numPeers", s.numPeers),
			zap.Int("numRemaining", s.numRemaining),
			zap.Int("numRunning", s.numRunning),
			zap.Int("availPeers", len(s.peers)))
	}

	return nil
}

func (s *splitSync) clearDeadPeers() {
	s.peers = slices.DeleteFunc(s.peers, func(p p2p.Peer) bool {
		return !s.peerSet.havePeer(p)
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
		for s.numRemaining > 0 && (s.sq.empty() || len(s.peers) == 0) {
			s.logger.Debug("QQQQQ: loop")
			if s.numRunning == 0 && len(s.peers) == 0 {
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

func getDelimiters(numPeers int) (h []types.Hash32) {
	if numPeers < 2 {
		return nil
	}
	inc := (uint64(0x80) << 56) / uint64(numPeers)
	h = make([]types.Hash32, numPeers-1)
	for i, v := 0, uint64(0); i < numPeers-1; i++ {
		v += inc
		binary.BigEndian.PutUint64(h[i][:], v<<1)
	}
	return h
}
