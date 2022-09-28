package tortoise

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// updateFromRerun must be called while holding appropriate mutex (see struct definition).
func (t *Tortoise) updateFromRerun(ctx context.Context) {
	logger := t.logger.WithContext(ctx).With()
	if t.update == nil {
		return
	}
	defer func() {
		t.update = nil
	}()
	var (
		current = t.trtl
		updated = t.update
		err     error
	)
	if current.last.After(updated.last) && err == nil {
		start := time.Now()
		logger.Info("tortoise received more layers while rerun was in progress. running a catchup",
			log.FieldNamed("last", current.last),
			log.FieldNamed("rerun-last", updated.last))

		err = catchupToCurrent(ctx, current, updated)
		if err != nil {
			logger.Error("catchup failed", log.Err(err))
		} else {
			logger.Info("catchup finished", log.Duration("duration", time.Since(start)))
		}
	}
	if err != nil {
		return
	}
	updated.cdb = current.cdb
	updated.logger = current.logger
	updated.mode = updated.mode.toggleRerun()
	t.trtl = updated
}

func (t *Tortoise) rerunLoop(ctx context.Context, period time.Duration) {
	timer := time.NewTimer(period)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			_ = t.rerun(ctx) // err is already logged with additional context
			timer.Reset(period)
		}
	}
}

func (t *Tortoise) rerun(ctx context.Context) error {
	t.mu.Lock()
	last := t.trtl.last
	historicallyVerified := t.trtl.historicallyVerified
	t.mu.Unlock()

	logger := t.logger.WithContext(ctx).WithFields(
		log.Bool("rerun", true),
	)

	start := time.Now()

	consensus := t.trtl.cloneTurtleParams()
	consensus.logger = logger
	consensus.init(ctx, types.GenesisLayer())
	consensus.last = last
	consensus.historicallyVerified = historicallyVerified
	consensus.mode = consensus.mode.toggleRerun()

	logger.With().Info("tortoise rerun started",
		log.Stringer("last_layer", last),
		log.Stringer("historically_verified", historicallyVerified),
		log.Stringer("mode", consensus.mode),
	)

	for lid := types.GetEffectiveGenesis().Add(1); !lid.After(last); lid = lid.Add(1) {
		if err := consensus.onLayer(ctx, lid); err != nil {
			return err
		}
	}
	consensus.historicallyVerified = consensus.verified
	logger.With().Info("tortoise rerun completed", last, log.Duration("duration", time.Since(start)))

	t.mu.Lock()
	defer t.mu.Unlock()
	t.update = consensus
	return nil
}

func catchupToCurrent(ctx context.Context, current, updated *turtle) error {
	for lid := updated.last.Add(1); !lid.After(current.last); lid = lid.Add(1) {
		if err := updated.onLayer(ctx, lid); err != nil {
			return err
		}
	}
	return nil
}
