package malsync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/malsync"
	"github.com/spacemeshos/go-spacemesh/system"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./syncer.go

type fetcher interface {
	SelectBestShuffled(int) []p2p.Peer
	GetMaliciousIDs(context.Context, p2p.Peer) ([]types.NodeID, error)
	system.MalfeasanceProofFetcher
}

type Opt func(*Syncer)

func WithLogger(logger *zap.Logger) Opt {
	return func(s *Syncer) {
		s.logger = logger
	}
}

type counter interface {
	Inc()
}

type noCounter struct{}

func (noCounter) Inc() {}

func WithPeerErrMetric(counter counter) Opt {
	return func(s *Syncer) {
		s.peerErrMetric = counter
	}
}

func withClock(clock clockwork.Clock) Opt {
	return func(s *Syncer) {
		s.clock = clock
	}
}

func DefaultConfig() Config {
	return Config{
		IDRequestInterval:  30 * time.Minute,
		MalfeasanceIDPeers: 3,
		MinSyncPeers:       3,
		MaxEpochFraction:   0.25,
		MaxBatchSize:       1000,
		RequestsLimit:      20,
		RetryInterval:      time.Minute,
	}
}

type Config struct {
	// IDRequestInterval specifies the interval for malfeasance proof id requests to the network.
	IDRequestInterval time.Duration `mapstructure:"id-request-interval"`

	// MalfeasanceIDPeers is the number of peers to fetch node IDs for malfeasance proofs from.
	MalfeasanceIDPeers int `mapstructure:"malfeasance-id-peers"`

	// Minimum number of peers to sync against for initial sync to be considered complete.
	MinSyncPeers int `mapstructure:"min-sync-peers"`

	// MaxEpochFraction specifies maximum fraction of epoch to expire before
	// synchronous malfeasance proof sync is needed upon startup.
	MaxEpochFraction float64 `mapstructure:"max-epoch-fraction"`

	// MaxBatchSize is the maximum number of node IDs to sync in a single request.
	MaxBatchSize int `mapstructure:"max-batch-size"`

	// RequestsLimit is the maximum number of requests for a single malfeasance proof.
	//
	// The purpose of it is to prevent peers from advertising invalid node ID and disappearing.
	// Which will make node ask other peers for invalid malfeasance proofs.
	// It will be reset to 0 once malfeasance proof is advertised again.
	RequestsLimit int `mapstructure:"requests-limit"`

	// RetryInterval specifies retry interval for the initial sync.
	RetryInterval time.Duration `mapstructure:"retry-interval"`
}

func WithConfig(cfg Config) Opt {
	return func(s *Syncer) {
		s.cfg = cfg
	}
}

type syncPeerSet map[p2p.Peer]struct{}

func (sps syncPeerSet) add(peer p2p.Peer) {
	sps[peer] = struct{}{}
}

func (sps syncPeerSet) clear() {
	maps.Clear(sps)
}

func (sps syncPeerSet) updateFrom(other syncPeerSet) {
	maps.Copy(sps, other)
}

// syncState stores malfeasance sync state.
type syncState struct {
	limit        int
	initial      bool
	state        map[types.NodeID]int
	syncingPeers syncPeerSet
	syncedPeers  syncPeerSet
}

func newSyncState(limit int, initial bool) *syncState {
	return &syncState{
		limit:        limit,
		initial:      initial,
		state:        make(map[types.NodeID]int),
		syncedPeers:  make(syncPeerSet),
		syncingPeers: make(syncPeerSet),
	}
}

func (sst *syncState) done() {
	if sst.initial {
		sst.syncedPeers.updateFrom(sst.syncingPeers)
		sst.syncingPeers.clear()
	}
	maps.Clear(sst.state)
}

func (sst *syncState) numSyncedPeers() int {
	return len(sst.syncedPeers)
}

func (sst *syncState) update(update malUpdate) {
	if sst.initial {
		sst.syncingPeers.add(update.peer)
	}
	for _, id := range update.nodeIDs {
		if _, found := sst.state[id]; !found {
			sst.state[id] = 0
		}
	}
}

func (sst *syncState) has(nodeID types.NodeID) bool {
	_, found := sst.state[nodeID]
	return found
}

func (sst *syncState) failed(nodeID types.NodeID) {
	// possibly temporary failure, count failed attempt
	n := sst.state[nodeID]
	if n >= 0 {
		sst.state[nodeID] = n + 1
	}
}

func (sst *syncState) rejected(nodeID types.NodeID) {
	// malfeasance proof didn't pass validation, no sense in requesting it anymore
	n := sst.state[nodeID]
	if n >= 0 {
		sst.state[nodeID] = sst.limit
	}
}

func (sst *syncState) downloaded(nodeID types.NodeID) {
	sst.state[nodeID] = -1
}

func (sst *syncState) missing(max int, has func(nodeID types.NodeID) (bool, error)) ([]types.NodeID, error) {
	r := make([]types.NodeID, 0, max)
	for nodeID, count := range sst.state {
		if count < 0 {
			continue // already downloaded
		}
		exists, err := has(nodeID)
		if err != nil {
			return nil, err
		}
		if exists {
			sst.downloaded(nodeID)
			continue
		}
		if count >= sst.limit {
			// unsuccessfully requested too many times
			delete(sst.state, nodeID)
			continue
		}
		r = append(r, nodeID)
		if len(r) == cap(r) {
			break
		}
	}
	return r, nil
}

type Syncer struct {
	logger        *zap.Logger
	cfg           Config
	fetcher       fetcher
	db            sql.Executor
	localdb       *localsql.Database
	clock         clockwork.Clock
	peerErrMetric counter
}

func New(fetcher fetcher, db sql.Executor, localdb *localsql.Database, opts ...Opt) *Syncer {
	s := &Syncer{
		logger:        zap.NewNop(),
		cfg:           DefaultConfig(),
		fetcher:       fetcher,
		db:            db,
		localdb:       localdb,
		clock:         clockwork.NewRealClock(),
		peerErrMetric: noCounter{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Syncer) shouldSync(epochStart, epochEnd time.Time) (bool, error) {
	timestamp, err := malsync.GetSyncState(s.localdb)
	if err != nil {
		return false, fmt.Errorf("error getting malfeasance sync state: %w", err)
	}
	if timestamp.Before(epochStart) {
		return true, nil
	}
	cutoff := epochEnd.Sub(epochStart).Seconds() * s.cfg.MaxEpochFraction
	return s.clock.Now().Sub(timestamp).Seconds() > cutoff, nil
}

func (s *Syncer) download(parent context.Context, initial bool) error {
	s.logger.Info("starting malfeasance proof sync", log.ZContext(parent))
	defer s.logger.Debug("malfeasance proof sync terminated", log.ZContext(parent))
	ctx, cancel := context.WithCancel(parent)
	eg, ctx := errgroup.WithContext(ctx)
	updates := make(chan malUpdate, s.cfg.MalfeasanceIDPeers)
	eg.Go(func() error {
		return s.downloadNodeIDs(ctx, initial, updates)
	})
	eg.Go(func() error {
		defer cancel()
		return s.downloadMalfeasanceProofs(ctx, initial, updates)
	})
	if err := eg.Wait(); err != nil {
		return err
	}
	return parent.Err()
}

func (s *Syncer) downloadNodeIDs(ctx context.Context, initial bool, updates chan<- malUpdate) error {
	interval := s.cfg.IDRequestInterval
	if initial {
		interval = 0
	}
	for {
		if interval != 0 {
			s.logger.Debug(
				"pausing between malfeasant node ID requests",
				zap.Duration("duration", interval))
			select {
			case <-ctx.Done():
				return nil
				// TODO(ivan4th) this has to be randomized in a followup
				// when sync will be schedulled in advance, in order to smooth out request rate across the network
			case <-s.clock.After(interval):
			}
		}

		peers := s.fetcher.SelectBestShuffled(s.cfg.MalfeasanceIDPeers)
		if len(peers) == 0 {
			s.logger.Debug(
				"don't have enough peers for malfeasance sync",
				zap.Int("nPeers", s.cfg.MalfeasanceIDPeers),
			)
			if interval == 0 {
				interval = s.cfg.RetryInterval
			}
			continue
		}

		var eg errgroup.Group
		for _, peer := range peers {
			eg.Go(func() error {
				malIDs, err := s.fetcher.GetMaliciousIDs(ctx, peer)
				if err != nil {
					if errors.Is(err, context.Canceled) {
						return ctx.Err()
					}
					s.peerErrMetric.Inc()
					s.logger.Warn("failed to download malfeasant node IDs",
						log.ZContext(ctx),
						zap.String("peer", peer.String()),
						zap.Error(err),
					)
					return nil
				}
				s.logger.Debug("downloaded malfeasant node IDs",
					log.ZContext(ctx),
					zap.String("peer", peer.String()),
					zap.Int("ids", len(malIDs)),
				)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case updates <- malUpdate{peer: peer, nodeIDs: malIDs}:
				}
				return nil
			})
		}

		if err := eg.Wait(); err != nil {
			return err
		}

		if interval == 0 {
			interval = s.cfg.RetryInterval
		}
	}
}

func (s *Syncer) updateState() error {
	if err := malsync.UpdateSyncState(s.localdb, s.clock.Now()); err != nil {
		return fmt.Errorf("error updating malsync state: %w", err)
	}

	return nil
}

func (s *Syncer) downloadMalfeasanceProofs(ctx context.Context, initial bool, updates <-chan malUpdate) error {
	var (
		update            malUpdate
		sst               = newSyncState(s.cfg.RequestsLimit, initial)
		nothingToDownload = true
		gotUpdate         = false
	)
	for {
		if nothingToDownload {
			sst.done()
			if initial && sst.numSyncedPeers() >= s.cfg.MinSyncPeers {
				if err := s.updateState(); err != nil {
					return err
				}
				s.logger.Info("initial sync of malfeasance proofs completed", log.ZContext(ctx))
				return nil
			} else if !initial && gotUpdate {
				if err := s.updateState(); err != nil {
					return err
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case update = <-updates:
				s.logger.Debug("malfeasance sync update",
					log.ZContext(ctx), zap.Int("count", len(update.nodeIDs)))
				sst.update(update)
				gotUpdate = true
			}
		} else {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case update = <-updates:
				s.logger.Debug("malfeasance sync update",
					log.ZContext(ctx), zap.Int("count", len(update.nodeIDs)))
				sst.update(update)
				gotUpdate = true
			default:
				// If we have some hashes to fetch already, don't wait for
				// another update
			}
		}
		batch, err := sst.missing(s.cfg.MaxBatchSize, func(nodeID types.NodeID) (bool, error) {
			// TODO(ivan4th): check multiple node IDs at once in a single SQL query
			isMalicious, err := identities.IsMalicious(s.db, nodeID)
			if err != nil && errors.Is(err, sql.ErrNotFound) {
				return false, nil
			}
			return isMalicious, err
		})
		if err != nil {
			return fmt.Errorf("error checking malfeasant node IDs: %w", err)
		}

		nothingToDownload = len(batch) == 0

		if len(batch) != 0 {
			s.logger.Debug("retrieving malfeasant identities",
				log.ZContext(ctx),
				zap.Int("count", len(batch)))
			err := s.fetcher.GetMalfeasanceProofs(ctx, batch)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return ctx.Err()
				}
				s.logger.Debug("failed to download malfeasance proofs",
					log.ZContext(ctx),
					log.NiceZapError(err),
				)
			}
			batchError := &fetch.BatchError{}
			if errors.As(err, &batchError) {
				for hash, err := range batchError.Errors {
					nodeID := types.NodeID(hash)
					switch {
					case !sst.has(nodeID):
						continue
					case errors.Is(err, pubsub.ErrValidationReject):
						sst.rejected(nodeID)
					default:
						sst.failed(nodeID)
					}
				}
			}
		} else {
			s.logger.Debug("no new malfeasant identities", log.ZContext(ctx))
		}
	}
}

func (s *Syncer) EnsureInSync(parent context.Context, epochStart, epochEnd time.Time) error {
	if shouldSync, err := s.shouldSync(epochStart, epochEnd); err != nil {
		return err
	} else if !shouldSync {
		return nil
	}
	return s.download(parent, true)
}

func (s *Syncer) DownloadLoop(parent context.Context) error {
	return s.download(parent, false)
}

type malUpdate struct {
	peer    p2p.Peer
	nodeIDs []types.NodeID
}
