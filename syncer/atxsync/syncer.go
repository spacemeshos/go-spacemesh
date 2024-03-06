package atxsync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/atxsync"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/system"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./syncer.go

type fetcher interface {
	SelectBestShuffled(int) []p2p.Peer
	PeerEpochInfo(context.Context, p2p.Peer, types.EpochID) (*fetch.EpochData, error)
	system.AtxFetcher
}

type Opt func(*Syncer)

func WithLogger(logger *zap.Logger) Opt {
	return func(s *Syncer) {
		s.logger = logger
	}
}

func DefaultConfig() Config {
	return Config{
		EpochInfoInterval: 30 * time.Minute,
		AtxsBatch:         1000,
		RequestsLimit:     20,
		EpochInfoPeers:    2,
	}
}

type Config struct {
	// EpochInfoInterval between epoch info requests to the network.
	EpochInfoInterval time.Duration `mapstructure:"epoch-info-request-interval"`
	// EpochInfoPeers is the number of peers we will ask for epoch info, every epoch info requests interval.
	EpochInfoPeers int `mapstructure:"epoch-info-peers"`

	// RequestsLimit is the maximum number of requests for single activation.
	//
	// The purpose of it is to prevent peers from advertising invalid atx and disappearing.
	// Which will make node ask other peers for invalid atx.
	// It will be reset to 0 once atx advertised again.
	RequestsLimit int `mapstructure:"requests-limit"`

	// AtxsBatch is the maximum number of atxs to sync in a single request.
	AtxsBatch int `mapstructure:"atxs-batch"`
}

func WithConfig(cfg Config) Opt {
	return func(s *Syncer) {
		s.cfg = cfg
	}
}

func New(fetcher fetcher, db sql.Executor, localdb *localsql.Database, opts ...Opt) *Syncer {
	s := &Syncer{
		logger:  zap.NewNop(),
		cfg:     DefaultConfig(),
		fetcher: fetcher,
		db:      db,
		localdb: localdb,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type Syncer struct {
	logger  *zap.Logger
	cfg     Config
	fetcher fetcher
	db      sql.Executor
	localdb *localsql.Database
}

func (s *Syncer) closeToTheEpoch(publish types.EpochID, timestamp, downloadUntil time.Time) bool {
	return timestamp.After(downloadUntil) || downloadUntil.Sub(timestamp) < 2*s.cfg.EpochInfoInterval
}

func (s *Syncer) Download(ctx context.Context, publish types.EpochID, downloadUntil time.Time) error {
	s.logger.Info("starting atx sync", log.ZContext(ctx), publish.Field().Zap())

	state, err := atxsync.GetSyncState(s.localdb, publish)
	if err != nil {
		return fmt.Errorf("failed to get state for epoch %v: %w", publish, err)
	}
	lastSuccess, err := atxsync.GetRequestTime(s.localdb, publish)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return fmt.Errorf("failed to get last request time for epoch %v: %w", publish, err)
	}
	// in case of immediate we will request epoch info without waiting EpochInfoInterval
	immediate := len(state) == 0 ||
		(errors.Is(err, sql.ErrNotFound) || !s.closeToTheEpoch(publish, lastSuccess, downloadUntil))

	ctx, cancel := context.WithCancel(ctx)
	eg, ctx := errgroup.WithContext(ctx)
	updates := make(chan epochUpdate, s.cfg.EpochInfoPeers)
	if len(state) == 0 {
		state = map[types.ATXID]int{}
	} else {
		updates <- epochUpdate{time: lastSuccess, update: state}
	}
	// termination requires two conditions:
	// - epoch info has to be successfully downloaded close to or after the epoch start
	// - all atxs from that epoch have to be downloaded or they are unavailable.
	//   atx is unavailable if it was requested more than RequestsLimit times, and no peer provided it.
	eg.Go(func() error {
		return s.downloadEpochInfo(ctx, publish, immediate, updates)
	})
	eg.Go(func() error {
		err := s.downloadAtxs(ctx, publish, downloadUntil, state, updates)
		cancel()
		return err
	})
	return eg.Wait()
}

func (s *Syncer) downloadEpochInfo(
	ctx context.Context,
	publish types.EpochID,
	immediate bool,
	updates chan epochUpdate,
) error {
	interval := s.cfg.EpochInfoInterval
	if immediate {
		interval = 0
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		// TODO(dshulyak) this has to be randomized in a followup
		// when sync will be schedulled in advance, in order to smooth out request rate across the network
		case <-time.After(interval):
		}

		peers := s.fetcher.SelectBestShuffled(s.cfg.EpochInfoPeers)
		if len(peers) == 0 {
			return fmt.Errorf("no peers available")
		}
		// do not run it concurrently, epoch info is large and will continue to grow
		for _, peer := range peers {
			epochData, err := s.fetcher.PeerEpochInfo(ctx, peer, publish)
			if err != nil || epochData == nil {
				if errors.Is(err, context.Canceled) {
					return nil
				}
				s.logger.Warn("failed to download epoch info",
					log.ZContext(ctx),
					publish.Field().Zap(),
					zap.String("peer", peer.String()),
					zap.Error(err),
				)
				continue
			}
			s.logger.Info("downloaded epoch info",
				log.ZContext(ctx),
				publish.Field().Zap(),
				zap.String("peer", peer.String()),
				zap.Int("atxs", len(epochData.AtxIDs)),
			)
			// adding hashes to fetcher is not useful as they overflow the cache and are not used
			// so we switch to asking best peers immediately
			update := make(map[types.ATXID]int, len(epochData.AtxIDs))
			for _, atx := range epochData.AtxIDs {
				update[atx] = 0
			}
			select {
			case <-ctx.Done():
				return nil
			case updates <- epochUpdate{time: time.Now(), update: update}:
			}
			// after first success switch to requests after interval
			interval = s.cfg.EpochInfoInterval
		}
	}
}

func (s *Syncer) downloadAtxs(
	ctx context.Context,
	publish types.EpochID,
	downloadUntil time.Time,
	state map[types.ATXID]int,
	updates chan epochUpdate,
) error {
	batch := make([]types.ATXID, 0, s.cfg.AtxsBatch)
	batch = batch[:0]
	var (
		downloaded           = map[types.ATXID]bool{}
		previouslyDownloaded = 0
		start                = time.Now()
		lastSuccess          time.Time
		progressTimestamp    = start
		nothingToDownload    = len(state) == 0
	)

	for {
		// waiting for update if there is nothing to download
		if nothingToDownload && s.closeToTheEpoch(publish, lastSuccess, downloadUntil) {
			s.logger.Info(
				"atx sync terminated",
				log.ZContext(ctx),
				publish.Field().Zap(),
				zap.Int("downloaded", len(downloaded)),
				zap.Int("total", len(state)),
				zap.Int("unavailable", len(state)-len(downloaded)),
				zap.Duration("duration", time.Since(start)),
			)
			return nil
		}
		if nothingToDownload {
			select {
			case <-ctx.Done():
				return nil
			case update := <-updates:
				lastSuccess = update.time
				for atx, count := range update.update {
					state[atx] = count
				}
			}
		} else {
			// otherwise check updates periodically but don't stop downloading
			select {
			case <-ctx.Done():
				return nil
			case update := <-updates:
				lastSuccess = update.time
				for atx, count := range update.update {
					state[atx] = count
				}
			default:
			}
		}

		for atx, requests := range state {
			if downloaded[atx] {
				continue
			}
			exists, err := atxs.Has(s.db, atx)
			if err != nil {
				return err
			}
			if exists {
				downloaded[atx] = true
				continue
			}
			if requests >= s.cfg.RequestsLimit {
				delete(state, atx)
				continue
			}
			batch = append(batch, atx)
			if len(batch) == cap(batch) {
				break
			}
		}
		nothingToDownload = len(batch) == 0

		// report progress every 10% or every 20 minutes
		if progress := float64(len(downloaded) - previouslyDownloaded); progress/float64(len(state)) > 0.1 ||
			time.Since(progressTimestamp) > 20*time.Minute {
			s.logger.Info(
				"atx sync progress",
				log.ZContext(ctx),
				publish.Field().Zap(),
				zap.Int("downloaded", len(downloaded)),
				zap.Int("total", len(state)),
				zap.Int("progress", int(progress)),
				zap.Float64("rate per sec", progress/time.Since(progressTimestamp).Seconds()),
			)
			previouslyDownloaded = len(downloaded)
			progressTimestamp = time.Now()
		}
		if len(batch) > 0 {
			if err := s.fetcher.GetAtxs(ctx, batch); err != nil {
				if errors.Is(err, context.Canceled) {
					return nil
				}
				s.logger.Debug("failed to download atxs", log.ZContext(ctx), log.NiceZapError(err))
				batchError := &fetch.BatchError{}
				if errors.As(err, &batchError) {
					for hash, err := range batchError.Errors {
						if errors.Is(err, server.ErrPeerResponseFailed) {
							state[types.ATXID(hash)]++
						}
					}
				}
			}
		}

		if err := s.localdb.WithTx(ctx, func(tx *sql.Tx) error {
			if err := atxsync.SaveRequestTime(tx, publish, lastSuccess); err != nil {
				return fmt.Errorf("failed to save request time: %w", err)
			}
			return atxsync.SaveSyncState(tx, publish, state, s.cfg.RequestsLimit)
		}); err != nil {
			return fmt.Errorf("failed to persist state for epoch %v: %w", publish, err)
		}
		batch = batch[:0]
	}
}

type epochUpdate struct {
	time   time.Time
	update map[types.ATXID]int
}
