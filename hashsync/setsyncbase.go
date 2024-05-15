package hashsync

import (
	"context"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"golang.org/x/sync/errgroup"
)

type syncKeyHandler func(ctx context.Context, k Ordered) error

type setSyncBase struct {
	r       requester
	is      ItemStore
	handler syncKeyHandler
	opts    []Option
	keyCh   chan Ordered
}

var _ syncBase = &setSyncBase{}

func newSetSyncBase(r requester, is ItemStore, handler syncKeyHandler, opts ...Option) *setSyncBase {
	return &setSyncBase{
		r:       r,
		is:      is,
		handler: handler,
		opts:    opts,
		keyCh:   make(chan Ordered),
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
func (ssb *setSyncBase) derive(p peer.ID) syncer {
	return &setSyncer{
		ItemStore: ssb.is.Copy(),
		r:         ssb.r,
		opts:      ssb.opts,
		p:         p,
		keyCh:     ssb.keyCh,
	}
}

// probe implements syncBase.
func (ssb *setSyncBase) probe(ctx context.Context, p peer.ID) (ProbeResult, error) {
	return Probe(ctx, ssb.r, p, ssb.is, nil, nil, ssb.opts...)
}

// run implements syncBase.
func (ssb *setSyncBase) run(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	doneCh := make(chan Ordered)
	beingProcessed := make(map[Ordered]struct{})
	for {
		select {
		case <-ctx.Done():
			return eg.Wait()
		case k := <-ssb.keyCh:
			if ssb.is.Has(k) {
				continue
			}
			if _, found := beingProcessed[k]; found {
				continue
			}
			eg.Go(func() error {
				defer func() {
					select {
					case <-ctx.Done():
					case doneCh <- k:
					}
				}()
				return ssb.handler(ctx, k)
			})
		case k := <-doneCh:
			delete(beingProcessed, k)
		}
	}
}

type setSyncer struct {
	ItemStore
	r     requester
	opts  []Option
	p     peer.ID
	keyCh chan<- Ordered
}

var (
	_ syncer    = &setSyncer{}
	_ ItemStore = &setSyncer{}
)

// peer implements syncer.
func (ss *setSyncer) peer() peer.ID {
	return ss.p
}

// sync implements syncer.
func (ss *setSyncer) sync(ctx context.Context, x, y *types.Hash32) error {
	return SyncStore(ctx, ss.r, ss.p, ss, x, y, ss.opts...)
}

// Add implements ItemStore.
func (ss *setSyncer) Add(ctx context.Context, k Ordered, v any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case ss.keyCh <- k:
	}
	return ss.ItemStore.Add(ctx, k, v)
}
