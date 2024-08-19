package hashsync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"golang.org/x/sync/singleflight"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type SyncKeyHandler func(ctx context.Context, k Ordered, peer p2p.Peer) error

type SetSyncBase struct {
	sync.Mutex
	ps      PairwiseSyncer
	is      ItemStore
	handler SyncKeyHandler
	waiting []<-chan singleflight.Result
	g       singleflight.Group
}

var _ SyncBase = &SetSyncBase{}

func NewSetSyncBase(ps PairwiseSyncer, is ItemStore, handler SyncKeyHandler) *SetSyncBase {
	return &SetSyncBase{
		ps:      ps,
		is:      is,
		handler: handler,
	}
}

// Count implements syncBase.
func (ssb *SetSyncBase) Count(ctx context.Context) (int, error) {
	// TODO: don't lock on db-bound operations
	ssb.Lock()
	defer ssb.Unlock()
	it, err := ssb.is.Min(ctx)
	if it == nil || err != nil {
		return 0, err
	}
	x, err := it.Key()
	if err != nil {
		return 0, err
	}
	info, err := ssb.is.GetRangeInfo(ctx, nil, x, x, -1)
	if err != nil {
		return 0, err
	}
	return info.Count, nil
}

// Derive implements syncBase.
func (ssb *SetSyncBase) Derive(p p2p.Peer) Syncer {
	ssb.Lock()
	defer ssb.Unlock()
	return &setSyncer{
		SetSyncBase: ssb,
		ItemStore:   ssb.is.Copy(),
		p:           p,
	}
}

// Probe implements syncBase.
func (ssb *SetSyncBase) Probe(ctx context.Context, p p2p.Peer) (ProbeResult, error) {
	// Use a snapshot of the store to avoid holding the mutex for a long time
	ssb.Lock()
	is := ssb.is.Copy()
	ssb.Unlock()

	return ssb.ps.Probe(ctx, p, is, nil, nil)
}

func (ssb *SetSyncBase) acceptKey(ctx context.Context, k Ordered, p p2p.Peer) error {
	ssb.Lock()
	defer ssb.Unlock()
	key := k.(fmt.Stringer).String()
	has, err := ssb.is.Has(ctx, k)
	if err != nil {
		return err
	}
	if !has {
		ssb.waiting = append(ssb.waiting,
			ssb.g.DoChan(key, func() (any, error) {
				err := ssb.handler(ctx, k, p)
				if err == nil {
					ssb.Lock()
					defer ssb.Unlock()
					err = ssb.is.Add(ctx, k)
				}
				return key, err
			}))
	}
	return nil
}

func (ssb *SetSyncBase) Wait() error {
	// At this point, the derived syncers should be done syncing, and we only want to
	// wait for the remaining handlers to complete. In case if some syncers happen to
	// be still running at this point, let's not fail too badly.
	// TODO: wait for any derived running syncers here, too
	ssb.Lock()
	waiting := ssb.waiting
	ssb.waiting = nil
	ssb.Unlock()
	var errs []error
	for _, w := range waiting {
		r := <-w
		ssb.g.Forget(r.Val.(string))
		errs = append(errs, r.Err)
	}
	return errors.Join(errs...)
}

type setSyncer struct {
	*SetSyncBase
	ItemStore
	p p2p.Peer
}

var (
	_ Syncer    = &setSyncer{}
	_ ItemStore = &setSyncer{}
)

// Peer implements syncer.
func (ss *setSyncer) Peer() p2p.Peer {
	return ss.p
}

// Sync implements syncer.
func (ss *setSyncer) Sync(ctx context.Context, x, y *types.Hash32) error {
	return ss.ps.SyncStore(ctx, ss.p, ss, x, y)
}

// Serve implements Syncer
func (ss *setSyncer) Serve(ctx context.Context, req []byte, stream io.ReadWriter) error {
	return ss.ps.Serve(ctx, req, stream, ss)
}

// Add implements ItemStore.
func (ss *setSyncer) Add(ctx context.Context, k Ordered) error {
	if err := ss.acceptKey(ctx, k, ss.p); err != nil {
		return err
	}
	return ss.ItemStore.Add(ctx, k)
}
