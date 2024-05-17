package hashsync

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"golang.org/x/sync/singleflight"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type syncKeyHandler func(ctx context.Context, k Ordered) error

type setSyncBase struct {
	ps      pairwiseSyncer
	is      ItemStore
	handler syncKeyHandler
	waiting []<-chan singleflight.Result
	g       singleflight.Group
}

var _ syncBase = &setSyncBase{}

func newSetSyncBase(ps pairwiseSyncer, is ItemStore, handler syncKeyHandler) *setSyncBase {
	return &setSyncBase{
		ps:      ps,
		is:      is,
		handler: handler,
	}
}

// count implements syncBase.
func (ssb *setSyncBase) count() int {
	it := ssb.is.Min()
	if it == nil {
		return 0
	}
	x := it.Key()
	return ssb.is.GetRangeInfo(nil, x, x, -1).Count
}

// derive implements syncBase.
func (ssb *setSyncBase) derive(p p2p.Peer) syncer {
	return &setSyncer{
		setSyncBase: ssb,
		ItemStore:   ssb.is.Copy(),
		p:           p,
	}
}

// probe implements syncBase.
func (ssb *setSyncBase) probe(ctx context.Context, p p2p.Peer) (ProbeResult, error) {
	return ssb.ps.probe(ctx, p, ssb.is, nil, nil)
}

func (ssb *setSyncBase) acceptKey(ctx context.Context, k Ordered) {
	key := k.(fmt.Stringer).String()
	if !ssb.is.Has(k) {
		ssb.waiting = append(ssb.waiting,
			ssb.g.DoChan(key, func() (any, error) {
				return key, ssb.handler(ctx, k)
			}))
	}
}

func (ssb *setSyncBase) wait() error {
	var errs []error
	for _, w := range ssb.waiting {
		r := <-w
		ssb.g.Forget(r.Val.(string))
		errs = append(errs, r.Err)
	}
	ssb.waiting = nil
	return errors.Join(errs...)
}

type setSyncer struct {
	*setSyncBase
	ItemStore
	p p2p.Peer
}

var (
	_ syncer    = &setSyncer{}
	_ ItemStore = &setSyncer{}
)

// peer implements syncer.
func (ss *setSyncer) peer() p2p.Peer {
	return ss.p
}

// sync implements syncer.
func (ss *setSyncer) sync(ctx context.Context, x, y *types.Hash32) error {
	return ss.ps.syncStore(ctx, ss.p, ss, x, y)
}

// Add implements ItemStore.
func (ss *setSyncer) Add(ctx context.Context, k Ordered) error {
	ss.acceptKey(ctx, k)
	return ss.ItemStore.Add(ctx, k)
}

type realPairwiseSyncer struct {
	r    requester
	opts []Option
}

var _ pairwiseSyncer = &realPairwiseSyncer{}

func (ps *realPairwiseSyncer) probe(ctx context.Context, peer p2p.Peer, is ItemStore, x, y *types.Hash32) (ProbeResult, error) {
	return Probe(ctx, ps.r, peer, is, x, y, ps.opts...)
}

func (ps *realPairwiseSyncer) syncStore(ctx context.Context, peer p2p.Peer, is ItemStore, x, y *types.Hash32) error {
	return SyncStore(ctx, ps.r, peer, is, x, y, ps.opts...)
}
