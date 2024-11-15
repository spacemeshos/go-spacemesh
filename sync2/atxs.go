package sync2

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
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

type ATXHandler struct {
	logger           *zap.Logger
	f                Fetcher
	clock            clockwork.Clock
	batchSize        int
	maxAttempts      int
	maxBatchRetries  int
	failedBatchDelay time.Duration
}

var _ multipeer.SyncKeyHandler = &ATXHandler{}

func NewATXHandler(
	logger *zap.Logger,
	f Fetcher,
	batchSize, maxAttempts, maxBatchRetries int,
	failedBatchDelay time.Duration,
	clock clockwork.Clock,
) *ATXHandler {
	if clock == nil {
		clock = clockwork.NewRealClock()
	}
	return &ATXHandler{
		f:                f,
		logger:           logger,
		clock:            clock,
		batchSize:        batchSize,
		maxAttempts:      maxAttempts,
		maxBatchRetries:  maxBatchRetries,
		failedBatchDelay: failedBatchDelay,
	}
}

func (h *ATXHandler) Receive(k rangesync.KeyBytes, peer p2p.Peer) (bool, error) {
	var id types.ATXID
	copy(id[:], k)
	h.f.RegisterPeerHash(peer, id.Hash32())
	return false, nil
}

func (h *ATXHandler) Commit(ctx context.Context, peer p2p.Peer, base, new rangesync.OrderedSet) error {
	h.logger.Debug("begin atx commit")
	defer h.logger.Debug("end atx commit")
	sr := new.Received()
	var firstK rangesync.KeyBytes
	numDownloaded := 0
	state := make(map[types.ATXID]int)
	for k := range sr.Seq {
		if firstK == nil {
			firstK = k
		} else if firstK.Compare(k) == 0 {
			break
		}
		found, err := base.Has(k)
		if err != nil {
			return fmt.Errorf("check if ATX exists: %w", err)
		}
		if found {
			continue
		}
		state[types.BytesToATXID(k)] = 0
	}
	if err := sr.Error(); err != nil {
		return fmt.Errorf("get item: %w", err)
	}
	total := len(state)
	items := make([]types.ATXID, 0, h.batchSize)
	startTime := h.clock.Now()
	batchAttemptsRemaining := h.maxBatchRetries
	for len(state) > 0 {
		items = items[:0]
		for id, n := range state {
			if n >= h.maxAttempts {
				h.logger.Debug("failed to download ATX: max attempts reached",
					zap.String("atx", id.ShortString()))
				delete(state, id)
				continue
			}
			items = append(items, id)
			if len(items) == h.batchSize {
				break
			}
		}
		if len(items) == 0 {
			break
		}

		var eg errgroup.Group
		recvCh := make(chan types.ATXID)
		doneCh := make(chan struct{})
		someSucceeded := false
		eg.Go(func() error {
			for {
				select {
				case id := <-recvCh:
					numDownloaded++
					someSucceeded = true
					delete(state, id)
				case <-doneCh:
					return nil
				}
			}
		})
		err := h.f.GetAtxs(ctx, items, system.WithRecvChannel(recvCh))
		close(doneCh)
		eg.Wait()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			batchError := &fetch.BatchError{}
			if errors.As(err, &batchError) {
				for hash, err := range batchError.Errors {
					if _, exists := state[types.ATXID(hash)]; !exists {
						continue
					}
					if errors.Is(err, pubsub.ErrValidationReject) {
						// if the atx invalid there's no point downloading it again
						state[types.ATXID(hash)] = h.maxAttempts
					} else {
						state[types.ATXID(hash)]++
					}
				}
			} else {
				h.logger.Debug("failed to download ATXs", zap.Error(err))
			}
		}
		if !someSucceeded {
			if batchAttemptsRemaining == 0 {
				return errors.New("failed to download ATXs: max batch retries reached")
			}
			batchAttemptsRemaining--
			h.logger.Debug("failed to download any ATXs: will retry batch",
				zap.Int("remaining", batchAttemptsRemaining),
				zap.Duration("delay", h.failedBatchDelay))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-h.clock.After(h.failedBatchDelay):
			}
		} else {
			batchAttemptsRemaining = h.maxBatchRetries
			elapsed := h.clock.Since(startTime)
			h.logger.Debug("fetched atxs",
				zap.Int("total", total),
				zap.Int("downloaded", numDownloaded),
				zap.Float64("rate per sec", float64(numDownloaded)/elapsed.Seconds()))
		}
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
) *MultiEpochATXSyncer {
	return &MultiEpochATXSyncer{
		logger:            logger,
		oldCfg:            oldCfg,
		newCfg:            newCfg,
		parallelLoadLimit: parallelLoadLimit,
		hss:               hss,
	}
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
			hs := s.hss.CreateHashSync(name, cfg, epoch)
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
		if epoch <= lastWaitEpoch {
			s.logger.Info("waiting for epoch to sync", zap.Uint32("epoch", epoch.Uint32()))
			if err := syncer.StartAndSync(ctx); err != nil {
				return lastSynced, fmt.Errorf("error syncing old ATXs: %w", err)
			}
			lastSynced = epoch
		} else {
			syncer.Start()
		}
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
	db sql.StateDatabase,
	f Fetcher,
	epoch types.EpochID,
	enableActiveSync bool,
) *P2PHashSync {
	curSet := dbset.NewDBSet(db, atxsTable(epoch), 32, cfg.MaxDepth)
	return NewP2PHashSync(
		logger, d, name, curSet, 32, f.Peers(),
		NewATXHandler(
			logger, f, cfg.BatchSize, cfg.MaxAttempts,
			cfg.MaxBatchRetries, cfg.FailedBatchDelay, nil),
		cfg, enableActiveSync)
}

func NewDispatcher(logger *zap.Logger, f Fetcher) *rangesync.Dispatcher {
	d := rangesync.NewDispatcher(logger)
	d.SetupServer(f.Host(), multipeer.Protocol, server.WithHardTimeout(20*time.Minute))
	return d
}

type ATXSyncSource struct {
	logger           *zap.Logger
	d                *rangesync.Dispatcher
	db               sql.StateDatabase
	f                Fetcher
	enableActiveSync bool
}

var _ HashSyncSource = &ATXSyncSource{}

func NewATXSyncSource(
	logger *zap.Logger,
	d *rangesync.Dispatcher,
	db sql.StateDatabase,
	f Fetcher,
	enableActiveSync bool,
) *ATXSyncSource {
	return &ATXSyncSource{logger: logger, d: d, db: db, f: f, enableActiveSync: enableActiveSync}
}

// CreateHashSync implements HashSyncSource.
func (as *ATXSyncSource) CreateHashSync(name string, cfg Config, epoch types.EpochID) HashSync {
	return NewATXSyncer(as.logger.Named(name), as.d, name, cfg, as.db, as.f, epoch, as.enableActiveSync)
}
