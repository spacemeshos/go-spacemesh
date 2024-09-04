package multipeer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"golang.org/x/sync/singleflight"

	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

type SyncKeyHandler func(ctx context.Context, k types.Ordered, peer p2p.Peer) error

type SetSyncBase struct {
	sync.Mutex
	ps      PairwiseSyncer
	os      rangesync.OrderedSet
	handler SyncKeyHandler
	waiting []<-chan singleflight.Result
	g       singleflight.Group
}

var _ SyncBase = &SetSyncBase{}

func NewSetSyncBase(ps PairwiseSyncer, os rangesync.OrderedSet, handler SyncKeyHandler) *SetSyncBase {
	return &SetSyncBase{
		ps:      ps,
		os:      os,
		handler: handler,
	}
}

// Count implements syncBase.
func (ssb *SetSyncBase) Count(ctx context.Context) (int, error) {
	// TODO: don't lock on db-bound operations
	ssb.Lock()
	defer ssb.Unlock()
	if empty, err := ssb.os.Empty(ctx); err != nil {
		return 0, fmt.Errorf("check if the set is empty: %w", err)
	} else if empty {
		return 0, nil
	}
	seq, err := ssb.os.Items(ctx)
	if err != nil {
		return 0, fmt.Errorf("get items: %w", err)
	}
	x, err := seq.First()
	if err != nil {
		return 0, fmt.Errorf("get first item: %w", err)
	}
	info, err := ssb.os.GetRangeInfo(ctx, x, x, -1)
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
		OrderedSet:  ssb.os.Copy(),
		p:           p,
	}
}

// Probe implements syncBase.
func (ssb *SetSyncBase) Probe(ctx context.Context, p p2p.Peer) (rangesync.ProbeResult, error) {
	// Use a snapshot of the store to avoid holding the mutex for a long time
	ssb.Lock()
	os := ssb.os.Copy()
	ssb.Unlock()

	return ssb.ps.Probe(ctx, p, os, nil, nil)
}

func (ssb *SetSyncBase) acceptKey(ctx context.Context, k types.Ordered, p p2p.Peer) error {
	ssb.Lock()
	defer ssb.Unlock()
	key := k.(fmt.Stringer).String()
	has, err := ssb.os.Has(ctx, k)
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
					err = ssb.os.Add(ctx, k)
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
	rangesync.OrderedSet
	p p2p.Peer
}

var (
	_ Syncer               = &setSyncer{}
	_ rangesync.OrderedSet = &setSyncer{}
)

// Peer implements syncer.
func (ss *setSyncer) Peer() p2p.Peer {
	return ss.p
}

// Sync implements syncer.
func (ss *setSyncer) Sync(ctx context.Context, x, y types.KeyBytes) error {
	return ss.ps.Sync(ctx, ss.p, ss, x, y)
}

// Serve implements Syncer
func (ss *setSyncer) Serve(ctx context.Context, req []byte, stream io.ReadWriter) error {
	return ss.ps.Serve(ctx, req, stream, ss)
}

// Add implements ItemStore.
func (ss *setSyncer) Add(ctx context.Context, k types.Ordered) error {
	if err := ss.acceptKey(ctx, k, ss.p); err != nil {
		return err
	}
	return ss.OrderedSet.Add(ctx, k)
}
