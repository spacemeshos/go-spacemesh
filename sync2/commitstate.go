package sync2

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

type ItemID interface {
	comparable
	log.ShortString
}

type CommitState[T ItemID] struct {
	logger        *zap.Logger
	handler       Handler[T]
	clock         clockwork.Clock
	mtx           sync.Mutex
	state         map[T]uint
	total         int
	numDownloaded int
	items         []T
	cfg           Config
	someSucceeded bool
}

func NewCommitState[T ItemID](
	logger *zap.Logger,
	handler Handler[T],
	clock clockwork.Clock,
	peer p2p.Peer,
	base rangesync.OrderedSet,
	received rangesync.SeqResult,
	cfg Config,
) (*CommitState[T], error) {
	state := make(map[T]uint)
	for k := range received.Seq {
		found, err := base.Has(k)
		if err != nil {
			return nil, fmt.Errorf("check if object exists: %w", err)
		}
		if found {
			continue
		}
		state[handler.Register(peer, k)] = 0
	}
	if err := received.Error(); err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}
	return &CommitState[T]{
		logger:  logger,
		handler: handler,
		clock:   clock,
		state:   state,
		total:   len(state),
		items:   make([]T, 0, cfg.BatchSize),
		cfg:     cfg,
	}, nil
}

func (cs *CommitState[T]) batch() []T {
	cs.items = cs.items[:0] // reuse the slice to reduce allocations
	for id := range cs.state {
		cs.items = append(cs.items, id)
		if uint(len(cs.items)) == cs.cfg.BatchSize {
			break
		}
	}
	return cs.items
}

func (cs *CommitState[T]) handleItem(id T, err error) {
	cs.mtx.Lock()
	defer cs.mtx.Unlock()
	switch {
	case err == nil:
		cs.numDownloaded++
		cs.someSucceeded = true
		delete(cs.state, id)
	case errors.Is(err, pubsub.ErrValidationReject):
		cs.logger.Debug("failed to download", log.ZShortStringer("id", id), zap.Error(err))
		delete(cs.state, id)
	case cs.state[id] >= cs.cfg.MaxAttempts-1:
		cs.logger.Debug("failed to download: max attempts reached", log.ZShortStringer("id", id))
		delete(cs.state, id)
	default:
		cs.state[id]++
	}
}

func (cs *CommitState[T]) Commit(ctx context.Context) error {
	startTime := cs.clock.Now()
	batchAttemptsRemaining := cs.cfg.MaxBatchRetries
	for len(cs.state) > 0 {
		cs.someSucceeded = false
		err := cs.handler.Get(ctx, cs.batch(), cs.handleItem)
		batchErr := &fetch.BatchError{}
		switch {
		case err == nil:
		case errors.Is(err, context.Canceled):
			return err
		case !errors.As(err, &batchErr):
			cs.logger.Debug("failed to download", zap.Error(err))
		}
		if !cs.someSucceeded {
			if batchAttemptsRemaining == 0 {
				return errors.New("failed to download: max batch retries reached")
			}
			batchAttemptsRemaining--
			cs.logger.Debug("failed to download any objects: will retry batch",
				zap.Uint("remaining", batchAttemptsRemaining),
				zap.Duration("delay", cs.cfg.FailedBatchDelay))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-cs.clock.After(cs.cfg.FailedBatchDelay):
				continue
			}
		}

		batchAttemptsRemaining = cs.cfg.MaxBatchRetries
		elapsed := cs.clock.Since(startTime)
		cs.logger.Debug("fetched objects",
			zap.Int("total", cs.total),
			zap.Int("downloaded", cs.numDownloaded),
			zap.Float64("rate per sec", float64(cs.numDownloaded)/elapsed.Seconds()))
	}
	return nil
}
