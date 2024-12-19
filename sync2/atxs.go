package sync2

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jonboulle/clockwork"
	"github.com/libp2p/go-libp2p/core/host"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/expr"
	"github.com/spacemeshos/go-spacemesh/sync2/dbset"
	"github.com/spacemeshos/go-spacemesh/sync2/multipeer"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/sqlstore"
	"github.com/spacemeshos/go-spacemesh/system"
)

const (
	proto = "sync/2"
)

type ATXHandler struct {
	logger *zap.Logger
	f      Fetcher
	clock  clockwork.Clock
	cfg    Config
}

var _ multipeer.SyncKeyHandler = &ATXHandler{}

func NewATXHandler(
	logger *zap.Logger,
	f Fetcher,
	cfg Config,
	clock clockwork.Clock,
) *ATXHandler {
	if clock == nil {
		clock = clockwork.NewRealClock()
	}
	return &ATXHandler{
		f:      f,
		logger: logger,
		clock:  clock,
		cfg:    cfg,
	}
}

type commitState struct {
	state         map[types.ATXID]uint
	total         int
	numDownloaded int
	items         []types.ATXID
}

func (h *ATXHandler) setupState(
	peer p2p.Peer,
	base rangesync.OrderedSet,
	received rangesync.SeqResult,
) (*commitState, error) {
	state := make(map[types.ATXID]uint)
	for k := range received.Seq {
		found, err := base.Has(k)
		if err != nil {
			return nil, fmt.Errorf("check if ATX exists: %w", err)
		}
		if found {
			continue
		}
		id := types.BytesToATXID(k)
		h.f.RegisterPeerHashes(peer, []types.Hash32{id.Hash32()})
		state[id] = 0
	}
	if err := received.Error(); err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}
	return &commitState{
		state: state,
		total: len(state),
		items: make([]types.ATXID, 0, h.cfg.BatchSize),
	}, nil
}

func (h *ATXHandler) getAtxs(ctx context.Context, cs *commitState) (bool, error) {
	cs.items = cs.items[:0] // reuse the slice to reduce allocations
	for id := range cs.state {
		cs.items = append(cs.items, id)
		if uint(len(cs.items)) == h.cfg.BatchSize {
			break
		}
	}
	someSucceeded := false
	var mtx sync.Mutex
	err := h.f.GetAtxs(ctx, cs.items, system.WithATXCallback(func(id types.ATXID, err error) {
		mtx.Lock()
		defer mtx.Unlock()
		switch {
		case err == nil:
			cs.numDownloaded++
			someSucceeded = true
			delete(cs.state, id)
		case errors.Is(err, pubsub.ErrValidationReject):
			h.logger.Debug("failed to download ATX",
				zap.String("atx", id.ShortString()), zap.Error(err))
			delete(cs.state, id)
		case cs.state[id] >= h.cfg.MaxAttempts-1:
			h.logger.Debug("failed to download ATX: max attempts reached",
				zap.String("atx", id.ShortString()))
			delete(cs.state, id)
		default:
			cs.state[id]++
		}
	}))
	return someSucceeded, err
}

func (h *ATXHandler) Commit(
	ctx context.Context,
	peer p2p.Peer,
	base rangesync.OrderedSet,
	received rangesync.SeqResult,
) error {
	h.logger.Debug("begin atx commit")
	defer h.logger.Debug("end atx commit")
	cs, err := h.setupState(peer, base, received)
	if err != nil {
		return err
	}
	startTime := h.clock.Now()
	batchAttemptsRemaining := h.cfg.MaxBatchRetries
	for len(cs.state) > 0 {
		someSucceeded, err := h.getAtxs(ctx, cs)
		batchErr := &fetch.BatchError{}
		switch {
		case err == nil:
		case errors.Is(err, context.Canceled):
			return err
		case !errors.As(err, &batchErr):
			h.logger.Debug("failed to download ATXs", zap.Error(err))
		}
		if !someSucceeded {
			if batchAttemptsRemaining == 0 {
				return errors.New("failed to download ATXs: max batch retries reached")
			}
			batchAttemptsRemaining--
			h.logger.Debug("failed to download any ATXs: will retry batch",
				zap.Uint("remaining", batchAttemptsRemaining),
				zap.Duration("delay", h.cfg.FailedBatchDelay))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-h.clock.After(h.cfg.FailedBatchDelay):
				continue
			}
		}

		batchAttemptsRemaining = h.cfg.MaxBatchRetries
		elapsed := h.clock.Since(startTime)
		h.logger.Debug("fetched atxs",
			zap.Int("total", cs.total),
			zap.Int("downloaded", cs.numDownloaded),
			zap.Float64("rate per sec", float64(cs.numDownloaded)/elapsed.Seconds()))
	}
	return nil
}

type MultiEpochATXSyncer struct {
	logger            *zap.Logger
	oldCfg            Config
	newCfg            Config
	parallelLoadLimit int
	hss               HashSyncSource
	newEpoch          types.EpochID
	atxSyncers        []HashSync
}

func NewMultiEpochATXSyncer(
	logger *zap.Logger,
	hss HashSyncSource,
	oldCfg, newCfg Config,
	parallelLoadLimit int,
) (*MultiEpochATXSyncer, error) {
	if !oldCfg.Validate(logger) || !newCfg.Validate(logger) {
		return nil, errors.New("invalid config")
	}
	return &MultiEpochATXSyncer{
		logger:            logger,
		oldCfg:            oldCfg,
		newCfg:            newCfg,
		parallelLoadLimit: parallelLoadLimit,
		hss:               hss,
	}, nil
}

func (s *MultiEpochATXSyncer) load(newEpoch types.EpochID) error {
	if len(s.atxSyncers) < int(newEpoch) {
		s.atxSyncers = append(s.atxSyncers, make([]HashSync, int(newEpoch)-len(s.atxSyncers))...)
	}
	s.newEpoch = newEpoch
	var eg errgroup.Group
	if s.parallelLoadLimit > 0 {
		eg.SetLimit(s.parallelLoadLimit)
	}
	for epoch := types.EpochID(1); epoch <= newEpoch; epoch++ {
		if s.atxSyncers[epoch-1] != nil {
			continue
		}
		eg.Go(func() error {
			name := fmt.Sprintf("atx-sync-%d", epoch)
			cfg := s.oldCfg
			if epoch == newEpoch {
				cfg = s.newCfg
			}
			hs, err := s.hss.CreateHashSync(name, cfg, epoch)
			if err != nil {
				return fmt.Errorf("create ATX syncer for epoch %d: %w", epoch, err)
			}
			if err := hs.Load(); err != nil {
				return fmt.Errorf("load ATX syncer for epoch %d: %w", epoch, err)
			}
			s.atxSyncers[epoch-1] = hs
			return nil
		})
	}
	return eg.Wait()
}

// EnsureSync ensures that ATX sync is active for all the epochs up to and including
// currentEpoch, and that all ATXs are
// synced up to and including lastWaitEpoch.
// If newEpoch argument is non-zero, faster but less memory efficient sync is used for
// that epoch, based on the newCfg (larger maxDepth).
// For other epochs, oldCfg is used which corresponds to slower but more memory efficient
// sync (smaller maxDepth).
// It returns the last epoch that was synced synchronously.
func (s *MultiEpochATXSyncer) EnsureSync(
	ctx context.Context,
	lastWaitEpoch, newEpoch types.EpochID,
) (lastSynced types.EpochID, err error) {
	if newEpoch != s.newEpoch && int(s.newEpoch) <= len(s.atxSyncers) && s.newEpoch > 0 {
		s.atxSyncers[s.newEpoch-1].Stop()
		s.atxSyncers[s.newEpoch-1] = nil
	}
	if err := s.load(newEpoch); err != nil {
		return lastSynced, err
	}
	for epoch := types.EpochID(1); epoch <= newEpoch; epoch++ {
		syncer := s.atxSyncers[epoch-1]
		if epoch > lastWaitEpoch {
			syncer.Start()
			continue
		}

		s.logger.Info("waiting for epoch to sync", zap.Uint32("epoch", epoch.Uint32()))
		if err := syncer.StartAndSync(ctx); err != nil {
			return lastSynced, fmt.Errorf("error syncing old ATXs: %w", err)
		}
		lastSynced = epoch
	}
	return lastSynced, nil
}

// Stop stops all ATX syncers.
func (s *MultiEpochATXSyncer) Stop() {
	for _, hs := range s.atxSyncers {
		hs.Stop()
	}
	s.atxSyncers = nil
	s.newEpoch = 0
}

func atxsTable(epoch types.EpochID) *sqlstore.SyncedTable {
	return &sqlstore.SyncedTable{
		TableName:       "atxs",
		IDColumn:        "id",
		TimestampColumn: "received",
		Filter:          expr.MustParse("epoch = ?"),
		Binder: func(s *sql.Statement) {
			s.BindInt64(1, int64(epoch))
		},
	}
}

func NewATXSyncer(
	logger *zap.Logger,
	d *rangesync.Dispatcher,
	name string,
	cfg Config,
	db sql.Database,
	f Fetcher,
	peers *peers.Peers,
	epoch types.EpochID,
	enableActiveSync bool,
) (*P2PHashSync, error) {
	curSet := dbset.NewDBSet(db, atxsTable(epoch), 32, int(cfg.MaxDepth))
	handler := NewATXHandler(logger, f, cfg, nil)
	return NewP2PHashSync(logger, d, name, curSet, 32, peers, handler, cfg, enableActiveSync)
}

func NewDispatcher(logger *zap.Logger, host host.Host, opts []server.Opt) *rangesync.Dispatcher {
	d := rangesync.NewDispatcher(logger)
	d.SetupServer(host, proto, opts...)
	return d
}

type ATXSyncSource struct {
	logger           *zap.Logger
	d                *rangesync.Dispatcher
	db               sql.Database
	f                Fetcher
	peers            *peers.Peers
	enableActiveSync bool
}

var _ HashSyncSource = &ATXSyncSource{}

func NewATXSyncSource(
	logger *zap.Logger,
	d *rangesync.Dispatcher,
	db sql.Database,
	f Fetcher,
	peers *peers.Peers,
	enableActiveSync bool,
) *ATXSyncSource {
	return &ATXSyncSource{logger: logger, d: d, db: db, f: f, peers: peers, enableActiveSync: enableActiveSync}
}

// CreateHashSync implements HashSyncSource.
func (as *ATXSyncSource) CreateHashSync(name string, cfg Config, epoch types.EpochID) (HashSync, error) {
	return NewATXSyncer(as.logger.Named(name), as.d, name, cfg, as.db, as.f, as.peers, epoch, as.enableActiveSync)
}
