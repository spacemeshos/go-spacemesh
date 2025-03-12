package multipeer

import (
	"context"
	"fmt"
	"io"
	"sync"

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
	os      rangesync.OrderedSet
	handler SyncKeyHandler
}

var _ SyncBase = &SetSyncBase{}

// NewSetSyncBase creates a new SetSyncBase.
func NewSetSyncBase(
	ps PairwiseSyncer,
	os rangesync.OrderedSet,
	handler SyncKeyHandler,
) *SetSyncBase {
	return &SetSyncBase{
		ps:      ps,
		os:      os,
		handler: handler,
	}
}

// Count implements SyncBase.
func (ssb *SetSyncBase) Count() (int, error) {
	// In most cases ssb.os.SetInfo will not access the database, so we're not holding
	// the lock for long here.
	ssb.mtx.Lock()
	defer ssb.mtx.Unlock()
	info, err := ssb.os.SetInfo()
	if err != nil {
		return 0, fmt.Errorf("get range info: %w", err)
	}
	return info.Count, nil
}

// Advance implements SyncBase.
func (ssb *SetSyncBase) Advance() error {
	ssb.mtx.Lock()
	defer ssb.mtx.Unlock()
	return ssb.os.Advance()
}

func (ssb *SetSyncBase) syncPeer(
	ctx context.Context,
	p p2p.Peer,
	toCall func(rangesync.OrderedSet) error,
) error {
	sr := rangesync.EmptySeqResult()
	if err := ssb.os.WithCopy(func(os rangesync.OrderedSet) error {
		if err := toCall(os); err != nil {
			return err
		}
		sr = os.Received()
		return nil
	}); err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	empty, err := sr.IsEmpty()
	if err != nil {
		return fmt.Errorf("check if the sequence result is empty: %w", err)
	}
	if !empty {
		if err := ssb.handler.Commit(ctx, p, ssb.os, sr); err != nil {
			return fmt.Errorf("commit: %w", err)
		}
		ssb.mtx.Lock()
		defer ssb.mtx.Unlock()
		return ssb.os.Advance()
	}
	return nil
}

func (ssb *SetSyncBase) Sync(ctx context.Context, p p2p.Peer, x, y rangesync.KeyBytes) error {
	return ssb.syncPeer(ctx, p, func(os rangesync.OrderedSet) error {
		return ssb.ps.Sync(ctx, p, os, x, y)
	})
}

func (ssb *SetSyncBase) Serve(ctx context.Context, p p2p.Peer, stream io.ReadWriter) error {
	return ssb.syncPeer(ctx, p, func(os rangesync.OrderedSet) error {
		return ssb.ps.Serve(ctx, stream, os)
	})
}

// Probe implements SyncBase.
func (ssb *SetSyncBase) Probe(ctx context.Context, p p2p.Peer) (pr rangesync.ProbeResult, err error) {
	// Use a snapshot of the store to avoid holding the mutex for a long time
	if err := ssb.os.WithCopy(func(os rangesync.OrderedSet) error {
		pr, err = ssb.ps.Probe(ctx, p, os, nil, nil)
		if err != nil {
			return fmt.Errorf("probing peer %s: %w", p, err)
		}
		return nil
	}); err != nil {
		return rangesync.ProbeResult{}, fmt.Errorf("using set copy for probe: %w", err)
	}

	return pr, nil
}
