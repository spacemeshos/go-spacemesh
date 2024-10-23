package multipeer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"golang.org/x/sync/singleflight"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

// SetSyncBase is a synchronization base which holds the original OrderedSet.
// For each peer, a Syncer is derived from the base which is used to synchronize against
// that peer only. This way, there's no propagation of any keys for which the actual data
// has not been yet received and validated.
type SetSyncBase struct {
	mtx     sync.Mutex
	ps      PairwiseSyncer
	os      OrderedSet
	handler SyncKeyHandler
	waiting []<-chan singleflight.Result
	g       singleflight.Group
}

var _ SyncBase = &SetSyncBase{}

// NewSetSyncBase creates a new SetSyncBase.
func NewSetSyncBase(ps PairwiseSyncer, os OrderedSet, handler SyncKeyHandler) *SetSyncBase {
	return &SetSyncBase{
		ps:      ps,
		os:      os,
		handler: handler,
	}
}

// Count implements SyncBase.
func (ssb *SetSyncBase) Count() (int, error) {
	// TODO: don't lock on db-bound operations
	ssb.mtx.Lock()
	defer ssb.mtx.Unlock()
	if empty, err := ssb.os.Empty(); err != nil {
		return 0, fmt.Errorf("check if the set is empty: %w", err)
	} else if empty {
		return 0, nil
	}
	x, err := ssb.os.Items().First()
	if err != nil {
		return 0, fmt.Errorf("get first item: %w", err)
	}
	info, err := ssb.os.GetRangeInfo(x, x)
	if err != nil {
		return 0, err
	}
	return info.Count, nil
}

// Derive implements SyncBase.
func (ssb *SetSyncBase) Derive(p p2p.Peer) Syncer {
	ssb.mtx.Lock()
	defer ssb.mtx.Unlock()
	return &setSyncer{
		SetSyncBase: ssb,
		OrderedSet:  ssb.os.Copy(true).(OrderedSet),
		p:           p,
		handler:     ssb.handler,
	}
}

// Probe implements SyncBase.
func (ssb *SetSyncBase) Probe(ctx context.Context, p p2p.Peer) (rangesync.ProbeResult, error) {
	// Use a snapshot of the store to avoid holding the mutex for a long time
	ssb.mtx.Lock()
	os := ssb.os.Copy(true)
	ssb.mtx.Unlock()
	defer os.(OrderedSet).Release()

	pr, err := ssb.ps.Probe(ctx, p, os, nil, nil)
	if err != nil {
		return rangesync.ProbeResult{}, err
	}
	return pr, os.(OrderedSet).Release()
}

func (ssb *SetSyncBase) receiveKey(k rangesync.KeyBytes, p p2p.Peer) error {
	ssb.mtx.Lock()
	defer ssb.mtx.Unlock()
	key := k.String()
	has, err := ssb.os.Has(k)
	if err != nil {
		return err
	}
	if !has {
		ssb.waiting = append(ssb.waiting,
			ssb.g.DoChan(key, func() (any, error) {
				addToOrig, err := ssb.handler.Receive(k, p)
				if err == nil && addToOrig {
					ssb.mtx.Lock()
					defer ssb.mtx.Unlock()
					err = ssb.os.Receive(k)
				}
				return key, err
			}))
	}
	return nil
}

// Wait waits for all the handlers used by derived syncers to finish.
func (ssb *SetSyncBase) Wait() error {
	// At this point, the derived syncers should be done syncing, and we only want to
	// wait for the remaining handlers to complete. In case if some syncers happen to
	// be still running at this point, let's not fail too badly.
	// TODO: wait for any derived running syncers here, too
	ssb.mtx.Lock()
	waiting := ssb.waiting
	ssb.waiting = nil
	ssb.mtx.Unlock()
	var errs []error
	for _, w := range waiting {
		r := <-w
		ssb.g.Forget(r.Val.(string))
		errs = append(errs, r.Err)
	}
	return errors.Join(errs...)
}

func (ssb *SetSyncBase) advance() error {
	ssb.mtx.Lock()
	defer ssb.mtx.Unlock()
	return ssb.os.Advance()
}

type setSyncer struct {
	*SetSyncBase
	OrderedSet
	p       p2p.Peer
	handler SyncKeyHandler
}

var (
	_ Syncer     = &setSyncer{}
	_ OrderedSet = &setSyncer{}
)

// Peer implements Syncer.
func (ss *setSyncer) Peer() p2p.Peer {
	return ss.p
}

// Sync implements Syncer.
func (ss *setSyncer) Sync(ctx context.Context, x, y rangesync.KeyBytes) error {
	if err := ss.ps.Sync(ctx, ss.p, ss, x, y); err != nil {
		return err
	}
	return ss.commit()
}

// Serve implements Syncer.
func (ss *setSyncer) Serve(ctx context.Context, stream io.ReadWriter) error {
	if err := ss.ps.Serve(ctx, stream, ss); err != nil {
		return err
	}
	return ss.commit()
}

// Receive implements OrderedSet.
func (ss *setSyncer) Receive(k rangesync.KeyBytes) error {
	if err := ss.receiveKey(k, ss.p); err != nil {
		return err
	}
	return ss.OrderedSet.Receive(k)
}

func (ss *setSyncer) commit() error {
	if err := ss.handler.Commit(ss.p, ss.SetSyncBase.os, ss.OrderedSet); err != nil {
		return err
	}
	return ss.SetSyncBase.advance()
}
